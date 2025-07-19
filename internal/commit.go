package app

import (
	"fmt"
	"strings"
	"time"
)

type Commit struct {
	CommittedAt time.Time
	Message     string
}

func NewCommit(committedAt time.Time, projectName string, shortHash string) *Commit {
	return &Commit{
		CommittedAt: committedAt,
		Message:     fmt.Sprintf("Project: %s commit: %s", projectName, shortHash),
	}
}

func ParseCommitMessage(message string) (projectName string, hash string, _ error) {
	const messagePartsCount = 4

	messageParts := strings.Split(message, " ")
	if len(messageParts) < messagePartsCount {
		return "", "", fmt.Errorf("wrong commit message: %s", message)
	}

	projectName = messageParts[1]
	hash = message

	return projectName, hash, nil
}
