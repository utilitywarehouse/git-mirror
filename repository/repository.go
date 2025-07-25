package repository

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/giturl"
	"github.com/utilitywarehouse/git-mirror/internal/lock"
	"github.com/utilitywarehouse/git-mirror/internal/utils"
)

const (
	defaultDirMode     fs.FileMode = os.FileMode(0755) // 'rwxr-xr-x'
	defaultRefSpec                 = "+refs/*:refs/*"
	MinAllowedInterval             = time.Second
	tracerSuffix                   = "-link-tracker"
)

var (
	ErrRepoMirrorFailed   = errors.New("repository mirror failed")
	ErrRepoWTUpdateFailed = errors.New("repository worktree update failed")

	// to parse output of "git ls-remote --symref origin HEAD"
	// ref: refs/heads/xxxx  HEAD
	remoteDefaultBranchRgx = regexp.MustCompile(`^ref:\s+([^\s]+)\s+HEAD`)
)

type gcMode string

const (
	GCAuto       = "auto"
	GCAlways     = "always"
	GCAggressive = "aggressive"
	GCOff        = "off"
)

// Repository represents the mirrored repository of the given remote.
// The implementation borrows heavily from https://github.com/kubernetes/git-sync.
// A Repository is safe for concurrent use by multiple goroutines.
type Repository struct {
	cmd           string                   // git exec path
	lock          lock.RWMutex             // repository will be locked during mirror
	gitURL        *giturl.URL              // parsed remote git URL
	remote        string                   // remote repo to mirror
	root          string                   // absolute path to the root where repo directory created
	linkRoot      string                   // absolute path to the root where repo worktree links are published
	dir           string                   // absolute path to the repo directory
	interval      time.Duration            // how long to wait between mirrors
	mirrorTimeout time.Duration            // the total time allowed for the mirror loop
	auth          *Auth                    // auth information including ssh key path
	gitGC         gcMode                   // garbage collection
	envs          []string                 // envs which will be passed to git commands
	running       bool                     // indicates if repository is running the mirror loop
	workTreeLinks map[string]*WorkTreeLink // list of worktrees which will be maintained
	stop, stopped chan bool                // chans to stop mirror loops
	queueMirror   chan struct{}            // chan to queue mirror run
	log           *slog.Logger

	githubAppToken          string
	githubAppTokenExpiresAt time.Time
}

// New creates new repository from the given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called.
func New(repoConf Config, gitExec string, envs []string, log *slog.Logger) (*Repository, error) {
	remoteURL := giturl.NormaliseURL(repoConf.Remote)

	gURL, err := giturl.Parse(remoteURL)
	if err != nil {
		return nil, err
	}

	if log == nil {
		log = slog.Default()
	}

	log = log.With("repo", gURL.Repo)

	if gitExec == "" {
		gitExec = exec.Command("git").String()
	}

	if !filepath.IsAbs(repoConf.Root) {
		return nil, fmt.Errorf("repository root '%s' must be absolute", repoConf.Root)
	}

	if repoConf.LinkRoot != "" && !filepath.IsAbs(repoConf.LinkRoot) {
		return nil, fmt.Errorf("repository link root set but path is not absolute  '%s'", repoConf.Root)
	}

	if repoConf.LinkRoot == "" {
		repoConf.LinkRoot = repoConf.Root
	}

	if repoConf.Interval < MinAllowedInterval {
		return nil, fmt.Errorf("provided interval between mirroring is too sort (%s), must be > %s", repoConf.Interval, MinAllowedInterval)
	}

	switch repoConf.GitGC {
	case GCAuto, GCAlways, GCAggressive, GCOff:
	default:
		return nil, fmt.Errorf("wrong gc value provided, must be one of %s, %s, %s, %s",
			GCAuto, GCAlways, GCAggressive, GCOff)
	}

	// we are going to create bare repo which caller cannot use directly
	// hence we can add repo dir (with .git suffix to indicate bare repo) to the provided root.
	// this also makes it safe to delete this dir and re-create it if needed
	// also this root could have been shared with other mirror repository (repoPool)
	repoDir := gURL.Repo
	if !strings.HasSuffix(repoDir, ".git") {
		repoDir += ".git"
	}
	repoDir = filepath.Join(DefaultRepoDir(repoConf.Root), repoDir)

	repo := &Repository{
		cmd:           gitExec,
		gitURL:        gURL,
		remote:        remoteURL,
		root:          repoConf.Root,
		linkRoot:      repoConf.LinkRoot,
		dir:           repoDir,
		interval:      repoConf.Interval,
		mirrorTimeout: repoConf.MirrorTimeout,
		auth:          &repoConf.Auth,
		log:           log,
		gitGC:         gcMode(repoConf.GitGC),
		envs:          envs,
		workTreeLinks: make(map[string]*WorkTreeLink),
		stop:          make(chan bool),
		stopped:       make(chan bool),

		// buffered chan so that run can be queued when mirror is already in progress
		// and max only 1 job in queue is needed
		queueMirror: make(chan struct{}, 1),
	}

	for _, wtc := range repoConf.Worktrees {
		if err := repo.AddWorktreeLink(wtc); err != nil {
			return nil, fmt.Errorf("unable to add worktree link err:%w", err)
		}
	}
	return repo, nil
}

// AddWorktreeLink adds workTree link to the mirror repository.
func (r *Repository) AddWorktreeLink(wtc WorktreeConfig) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if wtc.Link == "" {
		return fmt.Errorf("symlink path cannot be empty")
	}

	if v, ok := r.workTreeLinks[wtc.Link]; ok {
		return fmt.Errorf("worktree with given link already exits link:%s ref:%s", v.linkAbs, v.ref)
	}

	linkAbs := r.AbsoluteLink(wtc.Link)

	if wtc.Ref == "" {
		wtc.Ref = "HEAD"
	}

	wt := &WorkTreeLink{
		link:      wtc.Link,
		linkAbs:   linkAbs,
		ref:       wtc.Ref,
		pathspecs: wtc.Pathspecs,
		log:       r.log.With("worktree", wtc.Link),
	}

	// pathspecs must be sorted for for worktree equality checks
	slices.Sort(wt.pathspecs)

	r.workTreeLinks[wtc.Link] = wt
	return nil
}

// AbsoluteLink returns absolute link path based on repo's root
func (r *Repository) AbsoluteLink(link string) string {
	return utils.AbsLink(r.linkRoot, link)
}

// Remote returns repository's remote url
func (r *Repository) Remote() string {
	return r.remote
}

// Dir returns absolute path to the repo directory
func (r *Repository) Directory() string {
	return r.dir
}

// WorktreeLinks returns current clone of worktree maps
func (r *Repository) WorktreeLinks() map[string]*WorkTreeLink {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return maps.Clone(r.workTreeLinks)
}

// tryRLockWithContext will try to get read lock on the repository while
// monitoring given context. If context is done before acquiring read lock
// it will return an error
// this is needed because mirror() can take minutes to release lock and if
// given ctx has short timeout next call will fail anyway.
func (r *Repository) tryRLockWithContext(ctx context.Context) error {
	for {
		if r.lock.TryRLock() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err() // Timeout or cancellation
		default:
			time.Sleep(time.Second)
		}
	}
}

// Hash returns the hash of the given revision and for the path if specified.
func (r *Repository) Hash(ctx context.Context, ref, path string) (string, error) {
	if err := r.tryRLockWithContext(ctx); err != nil {
		return "", err
	}
	defer r.lock.RUnlock()

	return r.hash(ctx, ref, path)
}

// Subject returns commit subject of given commit hash
func (r *Repository) Subject(ctx context.Context, hash string) (string, error) {
	if err := r.tryRLockWithContext(ctx); err != nil {
		return "", err
	}
	defer r.lock.RUnlock()

	args := []string{"show", `--no-patch`, `--format=%s`, hash}
	msg, err := r.git(ctx, nil, "", args...)
	if err != nil {
		return "", err
	}
	return strings.Trim(msg, "'"), nil
}

// ChangedFiles returns path of the changed files for given commit hash
func (r *Repository) ChangedFiles(ctx context.Context, hash string) ([]string, error) {
	if err := r.tryRLockWithContext(ctx); err != nil {
		return nil, err
	}
	defer r.lock.RUnlock()

	args := []string{"show", `--name-only`, `--pretty=format:`, hash}
	msg, err := r.git(ctx, nil, "", args...)
	if err != nil {
		return nil, err
	}
	return strings.Split(msg, "\n"), nil
}

type CommitInfo struct {
	Hash         string
	ChangedFiles []string
}

// MergeCommits lists commits from the mergeCommitHash but not from the first
// parent of mergeCommitHash (mergeCommitHash^) in chronological order. (latest to oldest)
func (r *Repository) MergeCommits(ctx context.Context, mergeCommitHash string) ([]CommitInfo, error) {
	return r.ListCommitsWithChangedFiles(ctx, mergeCommitHash+"^", mergeCommitHash)
}

// BranchCommits lists commits from the tip of the branch but not from the HEAD
// of the repository in chronological order. (latest to oldest)
func (r *Repository) BranchCommits(ctx context.Context, branch string) ([]CommitInfo, error) {
	return r.ListCommitsWithChangedFiles(ctx, "HEAD", branch)
}

// ListCommitsWithChangedFiles returns path of the changed files for given commit hash
// list all the commits and files which are reachable from 'ref2', but not from 'ref1'
// The output is given in reverse chronological order.
func (r *Repository) ListCommitsWithChangedFiles(ctx context.Context, ref1, ref2 string) ([]CommitInfo, error) {
	if err := r.tryRLockWithContext(ctx); err != nil {
		return nil, err
	}
	defer r.lock.RUnlock()

	args := []string{"log", `--name-only`, `--pretty=format:%H`, ref1 + ".." + ref2}
	msg, err := r.git(ctx, nil, "", args...)
	if err != nil {
		return nil, err
	}
	return ParseCommitWithChangedFilesList(msg), nil
}

// ParseCommitWithChangedFilesList will parse following output of 'show/log'
// command with `--name-only`, `--pretty=format:%H` flags
//
//	72ea9c9de6963e97ac472d9ea996e384c6923cca
//
//	80e11d114dd3aa135c18573402a8e688599c69e0
//	one/readme.yaml
//	one/hello.tf
//	two/readme.yaml
func ParseCommitWithChangedFilesList(output string) []CommitInfo {
	commitCount := 0
	Commits := []CommitInfo{}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if IsFullCommitHash(line) {
			Commits = append(Commits, CommitInfo{Hash: line})
			commitCount += 1
			continue
		}
		// if line is not commit or empty then its assumed to be changed file name
		// also this file change belongs to the last commit
		if commitCount > 0 {
			Commits[commitCount-1].ChangedFiles = append(Commits[commitCount-1].ChangedFiles, line)
		}
	}

	return Commits
}

// ObjectExists returns error is given object is not valid or if it doesn't exists
func (r *Repository) ObjectExists(ctx context.Context, obj string) error {
	if err := r.tryRLockWithContext(ctx); err != nil {
		return err
	}
	defer r.lock.RUnlock()

	args := []string{"cat-file", `-e`, obj}
	_, err := r.git(ctx, nil, "", args...)
	return err
}

// Clone creates a single-branch local clone of the mirrored repository to a new location on
// disk. On success, it returns the hash of the new repository clone's HEAD.
// if pathspec is provided only those paths will be checked out.
// if ref is commit hash then pathspec will be ignored.
// if rmGitDir is true `.git` folder will be deleted after the clone.
// if dst not empty all its contents will be removed.
func (r *Repository) Clone(ctx context.Context, dst, ref string, pathspecs []string, rmGitDir bool) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}

	dst, err := filepath.Abs(dst)
	if err != nil {
		return "", fmt.Errorf("unable to convert given dst path '%s' to abs path err:%w", dst, err)
	}

	empty, err := dirIsEmpty(dst)
	if err != nil {
		return "", fmt.Errorf("unable to verify if dst is empty err:%w", err)
	}

	if !empty {
		// Git won't use this dir for clone.  We remove the contents rather than
		// the dir itself, because a common use-case is to have a volume mounted
		// at git.root, which makes removing it impossible.
		r.log.Info("repo directory was empty or failed checks", "path", dst)
		if err := removeDirContents(dst, r.log); err != nil {
			return "", fmt.Errorf("unable to wipe dst err:%w", err)
		}
	}

	if err := r.tryRLockWithContext(ctx); err != nil {
		return "", err
	}
	defer r.lock.RUnlock()

	// create a thin clone of a repository that only contains the history of the given revision
	// git clone --no-checkout --revision <ref> <repo_src> <dst>
	args := []string{"clone", "--no-checkout", "--revision", ref, r.dir, dst}
	if _, err := r.git(ctx, nil, "", args...); err != nil {
		return "", err
	}

	args = []string{"checkout", "HEAD"}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	// git checkout <branch> -- <pathspec>
	if _, err := r.git(ctx, nil, dst, args...); err != nil {
		return "", err
	}

	// get the hash of the repos HEAD
	args = []string{"log", "--pretty=format:%H", "-n", "1", "HEAD"}
	// git log --pretty=format:%H -n 1 HEAD
	hash, err := r.git(ctx, nil, dst, args...)
	if err != nil {
		return "", err
	}

	if rmGitDir {
		if err := os.RemoveAll(filepath.Join(dst, ".git")); err != nil {
			return "", fmt.Errorf("unable to delete git dir err:%w", err)
		}
	}

	return hash, nil
}

// IsRunning returns if repositories mirror loop is running or not
func (r *Repository) IsRunning() bool {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.running
}

// StartLoop mirrors repository periodically based on repo's mirror interval
func (r *Repository) StartLoop(ctx context.Context) {
	if r.IsRunning() {
		r.log.Error("mirror loop has already been started")
		return
	}

	r.lock.Lock()
	r.running = true
	r.lock.Unlock()

	r.log.Info("started repository mirror loop", "interval", r.interval)

	defer func() {
		r.lock.Lock()
		r.running = false
		r.lock.Unlock()

		close(r.stopped)
	}()

	for {
		// to avoid concurrent fetches on events or restarts
		time.Sleep(jitter(r.interval, 0.2))

		// to stop mirror running indefinitely we will use time-out
		mCtx, cancel := context.WithTimeout(ctx, r.mirrorTimeout)
		err := r.Mirror(mCtx)
		cancel()
		recordGitMirror(r.gitURL.Repo, err == nil)

		t := time.NewTimer(r.interval)
		select {
		case <-t.C:
		case <-r.queueMirror:
			t.Stop()
			r.log.Debug("triggering mirror")
		case <-ctx.Done():
			r.log.Info("context cancelled stopping mirror loop")
			return
		case <-r.stop:
			return
		}
	}
}

// QueueMirrorRun will queue a mirror run on repository. If a mirror is already
// in progress, another mirror will be triggered as soon as the current one completes.
// If a run is already queued then new request will be ignored
func (r *Repository) QueueMirrorRun() {
	select {
	case r.queueMirror <- struct{}{}:
	default:
		// mirror is already queued no action needed
	}
}

// StopLoop stops sync loop gracefully
func (r *Repository) StopLoop() {
	r.stop <- true
	<-r.stopped
	deleteMetrics(r.gitURL.Repo)
	r.log.Info("repository mirror loop stopped")
}

// Mirror will run mirror loop of the repository
//  1. init and validate if existing repo dir
//  2. fetch remote
//  3. ensure worktrees
//  4. cleanup if needed
func (r *Repository) Mirror(ctx context.Context) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	defer updateMirrorLatency(r.gitURL.Repo, time.Now())

	start := time.Now()

	if err := r.init(ctx); err != nil {
		r.log.Error("unable to init repo", "err", err)
		return ErrRepoMirrorFailed
	}

	refs, err := r.fetch(ctx)
	if err != nil {
		r.log.Error("unable to fetch repo", "err", err)
		return ErrRepoMirrorFailed
	}

	fetchTime := time.Since(start)

	var wtError error
	// worktree might need re-creating if it fails check
	// so always ensure worktree even if nothing fetched.
	// continue on error to make sync process more resilient
	for _, wl := range r.workTreeLinks {
		if err := r.ensureWorktree(ctx, wl); err != nil {
			r.log.Error("unable to ensure worktree", "link", wl.link, "err", err)
			wtError = ErrRepoWTUpdateFailed
		}
	}
	// ensure links after all worktrees are checked out for atomic changes
	for _, wl := range r.workTreeLinks {
		if err := r.ensureWorktreeLink(wl); err != nil {
			r.log.Error("unable to ensure worktree links", "link", wl.link, "err", err)
			wtError = ErrRepoWTUpdateFailed
		}
	}

	// in case of worktree error skip clean-up as it might remove existing
	// linked worktree which might not be desired
	if wtError != nil {
		return wtError
	}

	r.cleanup(ctx)

	r.log.Debug("mirror cycle complete", "time", time.Since(start), "fetch-time", fetchTime, "updated-refs", len(refs))
	return nil
}

// RemoveWorktreeLink removes workTree link from the mirror repository.
// it will remove published link as well even (if it failed to remove worktree)
func (r *Repository) RemoveWorktreeLink(link string) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	// To ensure atomic changes only remove link from the config, actual link
	// and worktree will be removed as part for mirror loop in cleanup process.
	delete(r.workTreeLinks, link)

	return nil
}

// worktreesRoot returns abs path for all the worktrees of the repo
// git uses `worktrees` folder for its on use hence we are using `.worktrees`
func (r *Repository) worktreesRoot() string {
	return filepath.Join(r.dir, ".worktrees")
}

// worktreePath generates path based on worktree link and hash.
// two worktree links can be on same ref but with diff pathspecs
// hence we cant just use tree hash as path
func (r *Repository) worktreePath(wl *WorkTreeLink, hash string) string {
	return filepath.Join(r.worktreesRoot(), wl.worktreeDirName(hash))
}

// init examines the git repo and determines if it is usable or not. If
// not, it will (re)initialize it.
// it will also make a remote call to get `symbolic-ref HEAD` of the remote
// to get default branch for the remote
func (r *Repository) init(ctx context.Context) error {
	_, err := os.Stat(r.dir)
	switch {
	case os.IsNotExist(err):
		// initial mirror
		r.log.Info("repo directory does not exist, creating it", "path", r.dir)
		if err := os.MkdirAll(r.dir, defaultDirMode); err != nil {
			return fmt.Errorf("unable to create repo dir err:%w", err)
		}
	case err != nil:
		return fmt.Errorf("unable to verify repo dir err:%w", err)
	default:
		// Make sure the directory we found is actually usable.
		if !r.sanityCheckRepo(ctx) {
			r.log.Error("repo directory was empty or failed checks, re-creating...", "path", r.dir)
			// Maybe a previous run crashed?  Git won't use this dir.
			// since we add own folder to given root path we could just delete whole dir
			// and re-create it
			if err := utils.ReCreate(r.dir); err != nil {
				return fmt.Errorf("unable to re-create repo dir err:%w", err)
			}
		} else {
			r.log.Log(ctx, -8, "existing repo directory is valid", "path", r.dir)
			return nil
		}
	}

	// create bare repository as we will use worktrees to checkout files
	r.log.Info("initializing repo directory", "path", r.dir)
	// git init -q --bare
	if _, err := r.git(ctx, nil, "", "init", "-q", "--bare"); err != nil {
		return fmt.Errorf("unable to init repo err:%w", err)
	}

	// create new remote "origin"
	// The "origin" remote has special meaning, like in relative-path submodules.
	// use --mirror=fetch as we want to create mirrored bare repository. it will make sure
	// everything in refs/* on the remote will be directly mirrored into refs/* in the local repository.
	// git remote add --mirror=fetch origin <remote>
	if _, err := r.git(ctx, nil, "", "remote", "add", "--mirror=fetch", "origin", r.remote); err != nil {
		return fmt.Errorf("unable to set remote err:%w", err)
	}

	// get default branch from remote and set it as local HEAD
	headBranch, err := r.getRemoteDefaultBranch(ctx)
	if err != nil {
		return fmt.Errorf("unable to get remote default branch err:%w", err)
	}

	// set local HEAD to remote HEAD/default branch
	// git symbolic-ref HEAD <headBranch>(refs/heads/master)
	if _, err := r.git(ctx, nil, "", "symbolic-ref", "HEAD", headBranch); err != nil {
		return fmt.Errorf("unable to set remote err:%w", err)
	}

	if !r.sanityCheckRepo(ctx) {
		return fmt.Errorf("can't initialize git repo directory")
	}

	return nil
}

// getRemoteDefaultBranch will run ls-remote to get HEAD of the remote
// and parse output to get default branch name
func (r *Repository) getRemoteDefaultBranch(ctx context.Context) (string, error) {
	envs := r.authEnv(ctx)

	// git ls-remote --symref origin HEAD
	out, err := r.git(ctx, envs, "", "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("unable to get default branch err:%w", err)
	}

	sections := remoteDefaultBranchRgx.FindStringSubmatch(out)

	if len(sections) == 2 {
		r.log.Info("fetched remote symbolic ref", "default-branch", sections[1])
		return sections[1], nil
	}

	return "", fmt.Errorf("unable to parse ls-remote output:%s sections:%s", out, sections)
}

// sanityCheckRepo tries to make sure that the repo dir is a valid git repository.
func (r *Repository) sanityCheckRepo(ctx context.Context) bool {
	// If it is empty, we are done.
	if empty, err := dirIsEmpty(r.dir); err != nil {
		r.log.Error("can't list repo directory", "path", r.dir, "err", err)
		return false
	} else if empty {
		r.log.Info("repo directory is empty", "path", r.dir)
		return false
	}

	// make sure repo is bare repository
	// git rev-parse --is-bare-repository
	if ok, err := r.git(ctx, nil, "", "rev-parse", "--is-bare-repository"); err != nil {
		r.log.Error("unable to verify bare repo", "path", r.dir, "err", err)
		return false
	} else if ok != "true" {
		r.log.Error("repo is not a bare repository", "path", r.dir)
		return false
	}

	// Check that this is actually the root of the repo.
	// git rev-parse --absolute-git-dir
	if root, err := r.git(ctx, nil, "", "rev-parse", "--absolute-git-dir"); err != nil {
		r.log.Error("can't get repo git dir", "path", r.dir, "err", err)
		return false
	} else {
		if root != r.dir {
			r.log.Error("repo directory is under another repo", "path", r.dir, "parent", root)
			return false
		}
	}

	// The "origin" remote has special meaning, like in relative-path submodules.
	// make sure origin exists with correct remote URL
	// git config --get remote.origin.url
	if stdout, err := r.git(ctx, nil, "", "config", "--get", "remote.origin.url"); err != nil {
		r.log.Error("can't get repo config remote.origin.url", "path", r.dir, "err", err)
		return false
	} else if stdout != r.remote {
		r.log.Error("repo configured with diff remote url", "path", r.dir, "remote.origin.url", stdout)
		return false
	}

	// verify origin's fetch refspec
	// git config --get remote.origin.fetch
	if stdout, err := r.git(ctx, nil, "", "config", "--get", "remote.origin.fetch"); err != nil {
		r.log.Error("can't get repo config remote.origin.fetch", "path", r.dir, "err", err)
		return false
	} else if stdout != defaultRefSpec {
		r.log.Error("repo configured with incorrect fetch refspec", "path", r.dir, "remote.origin.fetch", stdout)
		return false
	}

	// Consistency-check the repo.  Don't use --verbose because it can be
	// REALLY verbose.
	// git fsck --no-progress --connectivity-only
	if _, err := r.git(ctx, nil, "", "fsck", "--no-progress", "--connectivity-only"); err != nil {
		r.log.Error("repo fsck failed", "path", r.dir, "err", err)
		return false
	}

	return true
}

// fetch calls git fetch to update all references
func (r *Repository) fetch(ctx context.Context) ([]string, error) {
	// adding --porcelain so output can be parsed for updated refs
	// do not use -v output it will print all refs
	args := []string{"fetch", "origin", "--prune", "--no-progress", "--porcelain", "--no-auto-gc"}

	envs := r.authEnv(ctx)

	// git fetch origin --prune --no-progress --no-auto-gc
	out, err := r.git(ctx, envs, "", args...)
	return updatedRefs(out), err
}

// hash returns the hash of the given revision and for the path if specified.
func (r *Repository) hash(ctx context.Context, ref, path string) (string, error) {
	args := []string{"log", "--pretty=format:%H", "-n", "1", ref}
	if path != "" {
		args = append(args, "--", path)
	}
	// git log --pretty=format:%H -n 1 <ref> [-- <path>]
	return r.git(ctx, nil, "", args...)
}

// ensureWorktree will create / validate worktrees
func (r *Repository) ensureWorktree(ctx context.Context, wl *WorkTreeLink) error {
	// get remote hash from mirrored repo for the worktree link
	remoteHash, err := r.hash(ctx, wl.ref, "")
	if err != nil {
		return fmt.Errorf("unable to get hash for worktree:%s err:%w", wl.link, err)
	}

	// if we get empty remote hash so either given worktree ref do not exits yet or
	// its removed from the remote
	if remoteHash == "" {
		return fmt.Errorf("hash not found for given ref:%s for worktree:%s", wl.ref, wl.link)
	}

	var currentHash string

	// we do not care if we cant get old worktree path as we can create it
	wl.dir, err = wl.currentWorktree()
	if err != nil {
		// in case of error we create new worktree
		wl.log.Error("unable to get current worktree path", "err", err)
	}

	if wl.dir != "" {
		// get hash from the worktree folder
		currentHash, err = r.workTreeHash(ctx, wl, wl.dir)
		if err != nil {
			// in case of error we create new worktree
			wl.log.Error("unable to get current worktree hash", "err", err)
		}
	}

	if currentHash == remoteHash {
		if r.sanityCheckWorktree(ctx, wl) {
			return nil
		}
		wl.log.Error("worktree failed checks, re-creating...", "path", wl.dir)
	}

	wl.log.Info("worktree update required", "remoteHash", remoteHash, "currentHash", currentHash)
	newPath, err := r.createWorktree(ctx, wl, remoteHash)
	if err != nil {
		return fmt.Errorf("unable to create worktree for '%s' err:%w", wl.link, err)
	}
	// swap dir path on success
	wl.dir = newPath

	return nil
}

// ensureWorktreeLink will create/update worktree links
func (r *Repository) ensureWorktreeLink(wl *WorkTreeLink) error {
	if wl.dir == "" {
		return fmt.Errorf("worktree's checkout dir path not set")
	}

	// read symlink path of the given worktree link
	currentPath, err := wl.currentWorktree()
	if err != nil {
		// in case of error we create new worktree
		return fmt.Errorf("unable to get current worktree path err:%w", err)
	}

	if currentPath != wl.dir {
		// publish worktree to given symlink target
		if err = publishSymlink(wl.linkAbs, wl.dir); err != nil {
			return fmt.Errorf("unable to publish link err:%w", err)
		}
		wl.log.Info("publishing worktree link", "link", wl.link, "linkAbs", wl.linkAbs)
	}

	// create symlink in worktree root to keep track of target symlink
	// this will be used by cleanup process to remove target symlink if
	// worktree is removed
	var tracker = wl.dir + tracerSuffix
	trackedDstLink, _ := readlinkAbs(tracker)
	if wl.linkAbs != trackedDstLink {
		if err = publishSymlink(wl.dir+tracerSuffix, wl.linkAbs); err != nil {
			return fmt.Errorf("unable to publish link tracking symlink err:%w", err)
		}
	}
	return nil
}

// createWorktree will create new worktree using given hash
// if worktree already exists on then it will be removed and re-created
func (r *Repository) createWorktree(ctx context.Context, wl *WorkTreeLink, hash string) (string, error) {
	// generate path for worktree to checkout files
	wtPath := r.worktreePath(wl, hash)

	// remove any existing worktree as we cant create new worktree if path is
	// not empty
	// since wtPath contains git hash it will always be either new path or
	// existing worktree with failed sanity check
	if err := r.removeWorktree(ctx, wtPath); err != nil {
		return wtPath, err
	}

	wl.log.Info("creating worktree", "path", wtPath, "hash", hash)
	// git worktree add --force --detach --no-checkout <wt-path> <hash>
	_, err := r.git(ctx, nil, "", "worktree", "add", "--force", "--detach", "--no-checkout", wtPath, hash)
	if err != nil {
		return wtPath, err
	}

	// only checkout required path if specified
	args := []string{"checkout", hash}
	if len(wl.pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, wl.pathspecs...)
	}
	// git checkout <hash> -- <pathspec...>
	if _, err := r.git(ctx, nil, wtPath, args...); err != nil {
		return "", err
	}

	return wtPath, nil
}

// removeWorktree is used to remove a worktree and its folder if exits
func (r *Repository) removeWorktree(ctx context.Context, path string) error {
	// Clean up worktree, if needed.
	_, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return err
	}

	r.log.Info("removing worktree", "path", path)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("error removing directory: %w", err)
	}
	// git worktree prune -v
	if _, err := r.git(ctx, nil, "", "worktree", "prune", "--verbose"); err != nil {
		return err
	}
	return nil
}

// cleanup removes old worktrees and runs git's garbage collection.
func (r *Repository) cleanup(ctx context.Context) bool {
	var success bool

	// Clean up stale worktrees and links.
	success = r.removeStaleWorktreeLinks()

	if _, err := r.removeStaleWorktrees(); err != nil {
		r.log.Error("cleanup: unable to remove stale worktree", "err", err)
		success = false
	}

	// Let git know we don't need those old commits any more.
	// git worktree prune -v
	if _, err := r.git(ctx, nil, "", "worktree", "prune", "--verbose"); err != nil {
		r.log.Error("cleanup: git worktree prune failed", "err", err)
		success = false
	}

	// Expire old refs.
	// git reflog expire --expire-unreachable=all --all
	if _, err := r.git(ctx, nil, "", "reflog", "expire", "--expire-unreachable=all", "--all"); err != nil {
		r.log.Error("cleanup: git reflog failed", "err", err)
		success = false
	}

	// Run GC if needed.
	if r.gitGC != GCOff {
		args := []string{"gc"}
		switch r.gitGC {
		case GCAuto:
			args = append(args, "--auto")
		case GCAlways:
			// no extra flags
		case GCAggressive:
			args = append(args, "--aggressive")
		}
		if _, err := r.git(ctx, nil, "", args...); err != nil {
			r.log.Error("cleanup: git gc failed", "err", err)
			success = false
		}
	}

	return success
}

// removeStaleWorktreeLinks will clear stale links by comparing links in config
// and tracker links on disk.
func (r *Repository) removeStaleWorktreeLinks() bool {
	success := true
	var configLinks []string

	for _, wl := range r.workTreeLinks {
		configLinks = append(configLinks, wl.linkAbs)
	}

	// map of abs path of tracker to the abs path of its link
	onDiskTrackedLinks := make(map[string]string)
	dirents, err := os.ReadDir(r.worktreesRoot())
	if err != nil {
		r.log.Error("unable to read link worktree root dir", "err", err)
		return false
	}

	for _, fi := range dirents {
		if fi.IsDir() {
			continue
		}

		if strings.HasSuffix(fi.Name(), tracerSuffix) {
			tracker := filepath.Join(r.worktreesRoot(), fi.Name())
			trackedDstLink, err := readlinkAbs(tracker)
			if err != nil {
				r.log.Error("unable to read link tracking symlink", "file", fi.Name(), "err", err)
				success = false
				continue
			}
			onDiskTrackedLinks[tracker] = trackedDstLink
		}
	}

	for tracker, trackedDstLink := range onDiskTrackedLinks {
		if slices.Contains(configLinks, trackedDstLink) {
			continue
		}

		// read link of  tracked dst file and confirm its a actually pointing
		// to the stale worktree
		if wtPath, err := readlinkAbs(trackedDstLink); err == nil {
			if wtPath == strings.TrimSuffix(tracker, tracerSuffix) {
				if err := os.Remove(trackedDstLink); err != nil {
					r.log.Error("unable to remove stale published link", "link", trackedDstLink, "err", err)
					success = false
					continue
				}
			}
		}

		if err := os.Remove(tracker); err != nil {
			r.log.Error("unable to remove stale link tracker file", "tracker", tracker, "trackedLink", trackedDstLink, "err", err)
			success = false
			continue
		}

		r.log.Info("stale link removed", "link", trackedDstLink)
	}

	return success
}

func (r *Repository) removeStaleWorktrees() (int, error) {
	var currentWTDirs []string

	for _, wt := range r.workTreeLinks {
		t, err := wt.currentWorktree()
		if err != nil {
			r.log.Error("unable to read worktree link", "worktree", wt.link, "err", err)
			continue
		}
		if t != "" {
			_, wtDir := utils.SplitAbs(t)
			currentWTDirs = append(currentWTDirs, wtDir)
			currentWTDirs = append(currentWTDirs, wtDir+tracerSuffix)
		}
	}

	count := 0
	err := removeDirContentsIf(r.worktreesRoot(), r.log, func(fi os.FileInfo) (bool, error) {
		// only keep files related to current worktrees
		if !slices.Contains(currentWTDirs, fi.Name()) {
			count++
			r.log.Info("removing stale file/folder", "worktree", fi.Name())
			return true, nil
		}
		return false, nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return count, nil
}

// git runs git command with given arguments on given CWD
func (r *Repository) git(ctx context.Context, envs []string, cwd string, args ...string) (string, error) {
	if cwd == "" {
		cwd = r.dir
	}
	return utils.RunCommand(ctx, r.log, append(r.envs, envs...), cwd, r.cmd, args...)
}
