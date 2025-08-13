package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/auth"
	"github.com/utilitywarehouse/git-mirror/giturl"
)

const loadCredsScript = `#!/bin/sh

case "$1" in
  Username*) echo "$REPO_USERNAME" ;;
  Password*) echo "$REPO_PASSWORD" ;;
esac
`

func (r *Repository) authEnv(ctx context.Context) []string {
	var envs []string

	if giturl.IsSCPURL(r.remote) || giturl.IsSSHURL(r.remote) {
		envs = append(envs, r.gitSSHCommand())
		return envs
	}

	// if url not ssh or https nothing to set
	if !giturl.IsHTTPSURL(r.remote) {
		return nil
	}

	var username, password string
	switch {
	// if username & password is set use that
	case r.auth.Username != "" && r.auth.Password != "":
		username = r.auth.Username
		password = r.auth.Password

	// if only password (token) is set use that
	case r.auth.Password != "":
		username = "-" // username is required
		password = r.auth.Password

	// if github app config is set use that token
	case r.auth.GithubAppInstallationID != "" && r.gitURL.Host == "github.com":
		// github matches repo name without `.git` for permission for token req
		token, err := r.getGithubAppToken(ctx, strings.TrimSuffix(r.gitURL.Repo, ".git"))
		if err != nil {
			r.log.Error("unable to get github app token", "err", err)
			return nil
		}
		username = "-" // username is required
		password = token

	default:
		return nil
	}

	loadCredsScript, err := r.ensureCredsLoader()
	if err != nil {
		r.log.Error("unable to write load creds script file", "err", err)
		return nil
	}

	envs = append(envs, fmt.Sprintf(`GIT_ASKPASS=%s`, loadCredsScript))
	envs = append(envs, fmt.Sprintf(`REPO_USERNAME=%s`, username))
	envs = append(envs, fmt.Sprintf(`REPO_PASSWORD=%s`, password))

	return envs
}

func (r *Repository) ensureCredsLoader() (string, error) {
	credsLoader := filepath.Join(r.dir, "git-mirror-creds-loader.sh")

	_, err := os.Stat(credsLoader)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(credsLoader, []byte(loadCredsScript), 0750); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("unable to check if script file exits err:%w", err)
	}

	return credsLoader, nil
}

// gitSSHCommand returns the environment variable to be used for configuring
// git over ssh.
func (r *Repository) gitSSHCommand() string {
	sshKeyPath := r.auth.SSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = "/dev/null"
	}
	knownHostsOptions := "-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"
	if r.auth.SSHKeyPath != "" && r.auth.SSHKnownHostsPath != "" {
		knownHostsOptions = fmt.Sprintf("-o UserKnownHostsFile=%s", r.auth.SSHKnownHostsPath)
	}
	return fmt.Sprintf(`GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=%s %s`, sshKeyPath, knownHostsOptions)
}

func (r *Repository) getGithubAppToken(ctx context.Context, repo string) (string, error) {
	// return token if current token is valid for next 10 min
	if r.githubAppTokenExpiresAt.After(time.Now().UTC().Add(10 * time.Minute)) {
		return r.githubAppToken, nil
	}

	permissions := auth.GithubAppTokenReqPermissions{
		Repositories: []string{repo},
		Permissions:  map[string]string{"contents": "read"},
	}

	token, err := auth.GithubAppInstallationToken(ctx,
		r.auth.GithubAppID, r.auth.GithubAppInstallationID, r.auth.GithubAppPrivateKeyPath,
		permissions)
	if err != nil {
		return "", err
	}

	r.githubAppToken = token.Token
	r.githubAppTokenExpiresAt = token.ExpiresAt

	r.log.Debug("new github app access token created")

	return r.githubAppToken, nil
}
