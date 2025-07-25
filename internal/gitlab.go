package app

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"gitlab.com/gitlab-org/api/client-go"
)

const (
	maxCommits = 1000
)

type GitLab struct {
	logger *log.Logger

	gitlabClient *gitlab.Client
}

func NewGitLab(logger *log.Logger, gitlabClient *gitlab.Client) *GitLab {
	return &GitLab{
		logger:       logger,
		gitlabClient: gitlabClient,
	}
}

func (s *GitLab) CurrentUser(ctx context.Context) (*User, error) {
	user, _, err := s.gitlabClient.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	emails, _, err := s.gitlabClient.Users.ListEmails(gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user emails: %w", err)
	}

	emailAddresses := make([]string, 0, len(emails))
	for _, email := range emails {
		emailAddresses = append(emailAddresses, email.Email)
	}

	if len(emailAddresses) == 0 {
		log.Printf("No emails returned from ListEmails; using fallback user.Email: %s", user.Email)
		emailAddresses = []string{user.Email}
	}

	return &User{
		Name:      user.Name,
		Emails:    emailAddresses,
		Username:  user.Username,
		CreatedAt: *user.CreatedAt,
	}, nil
}

func (s *GitLab) FetchProjectPage(ctx context.Context, page int, user *User, idAfter int,
) (_ []int, nextPage int, _ error) {
	const perPage = 100

	projects := make([]int, 0, perPage)

	opt := &gitlab.ListProjectsOptions{
		ListOptions: gitlab.ListOptions{
			Page:    page,
			PerPage: perPage,
		},
		OrderBy:    gitlab.Ptr("id"),
		Sort:       gitlab.Ptr("asc"),
		Simple:     gitlab.Ptr(true),
		Membership: gitlab.Ptr(true),
		IDAfter:    gitlab.Ptr(idAfter),
	}

	projs, resp, err := s.gitlabClient.Projects.ListProjects(opt, gitlab.WithContext(ctx))
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}

	for _, proj := range projs {
		if !s.HasUserContributions(ctx, user, proj.ID) {
			continue
		}

		s.logger.Printf("Fetching project: %d", proj.ID)

		projects = append(projects, proj.ID)
	}

	if resp.CurrentPage >= resp.TotalPages {
		return projects, 0, nil
	}

	return projects, resp.NextPage, nil
}

func (s *GitLab) HasUserContributions(ctx context.Context, user *User, projectID int) bool {
	const perPage = 50

	opt := &gitlab.ListContributorsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: perPage,
			Page:    1,
		},
	}

	for {
		contrs, resp, err := s.gitlabClient.Repositories.Contributors(projectID, opt, gitlab.WithContext(ctx))
		if err != nil {
			s.logger.Printf("get contributors for project %d: %v", projectID, err)
			return false
		}

		for _, c := range contrs {
			if contains(user.Emails, c.Email) {
				return true
			}
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		opt.Page = resp.NextPage
	}

	return false
}

func (s *GitLab) FetchCommits(ctx context.Context, user *User, projectID int, since time.Time,
) ([]*Commit, error) {
	commits := make([]*Commit, 0, maxCommits)

	const commitsPerPage = 100

	project, _, err := s.gitlabClient.Projects.GetProject(projectID, nil, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get project name by ID %d: %w", projectID, err)
	}
	projectName := project.Name

	page := 1
	for page > 0 {
		cms, nextPage, err := s.fetchCommitPage(ctx, user, page, commitsPerPage, since, projectID, projectName)
		if err != nil {
			return nil, fmt.Errorf("fetch one commit page: %w", err)
		}

		commits = append(commits, cms...)
		page = nextPage
	}

	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}

	return commits, nil
}

func (s *GitLab) fetchCommitPage(
	ctx context.Context, user *User, page, perPage int, since time.Time, projectID int, projectName string,
) (commits []*Commit, nextPage int, err error) {
	commits = make([]*Commit, 0, perPage)

	opt := &gitlab.ListCommitsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: perPage,
			Page:    page,
		},
		All: gitlab.Ptr(true),
	}

	if !since.IsZero() {
		opt.Since = gitlab.Ptr(since)
	}

	comms, resp, err := s.gitlabClient.Commits.ListCommits(projectID, opt, gitlab.WithContext(ctx))
	if err != nil {
		return nil, 0, fmt.Errorf("get commits for project %d: %w", projectID, err)
	}

	for _, comm := range comms {
		if !contains(user.Emails, comm.AuthorEmail) || !contains(user.Emails, comm.CommitterEmail) {
			continue
		}

		s.logger.Printf("fetching commit: %s %s", comm.Message, comm.CommittedDate)

		commits = append(commits, NewCommit(*comm.CommittedDate, projectName, comm.Message))
	}

	if resp.TotalPages == 0 {
		return commits, resp.NextPage, nil
	}

	if resp.CurrentPage >= resp.TotalPages {
		return commits, 0, nil
	}

	return commits, resp.NextPage, nil
}

func contains(s []string, v string) bool {
	return slices.ContainsFunc(s, func(item string) bool {
		return strings.EqualFold(item, v)
	})
}
