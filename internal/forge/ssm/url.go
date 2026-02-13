package ssm

import (
	"fmt"
	"regexp"
)

// ssmURLRegex matches SSM URLs in HTTPS format.
// Examples:
//
//	https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo.git
//	https://us-central1-git.us-central1.sourcemanager.dev/my-project/my-repo
var ssmURLRegex = regexp.MustCompile(`([a-z0-9-]+)-git\.([a-z0-9-]+)\.sourcemanager\.dev/([^/]+)/([^/]+?)(\.git)?$`)

// IsSSMURL returns true if the URL matches the SSM URL pattern.
func IsSSMURL(url string) bool {
	return ssmURLRegex.MatchString(url)
}

// ParseSSMURL extracts instance, location, project, and repo from an SSM URL.
func ParseSSMURL(url string) (instance, location, project, repo string, err error) {
	matches := ssmURLRegex.FindStringSubmatch(url)
	if matches == nil || len(matches) < 5 {
		return "", "", "", "", fmt.Errorf("could not parse SSM URL: %s", url)
	}
	instance = matches[1]
	location = matches[2]
	project = matches[3]
	repo = matches[4]
	return instance, location, project, repo, nil
}

// NormalizeSSMURL converts an SSM remote URL to its canonical HTTPS form.
func NormalizeSSMURL(url string) (string, error) {
	instance, location, project, repo, err := ParseSSMURL(url)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s-git.%s.sourcemanager.dev/%s/%s", instance, location, project, repo), nil
}

// ResourceName returns the full SSM resource name for a repository.
func ResourceName(project, location, repo string) string {
	return fmt.Sprintf("projects/%s/locations/%s/repositories/%s", project, location, repo)
}
