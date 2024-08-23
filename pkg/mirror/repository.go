package mirror

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	"github.com/utilitywarehouse/git-mirror/pkg/lock"
)

const (
	defaultDirMode     fs.FileMode = os.FileMode(0755) // 'rwxr-xr-x'
	defaultRefSpec                 = "+refs/*:refs/*"
	minAllowedInterval             = time.Second
)

var (
	gitExecutablePath string
	staleTimeout      time.Duration = 10 * time.Second // time for stale worktrees to be cleaned up

	// to parse output of "git ls-remote --symref origin HEAD"
	// ref: refs/heads/xxxx  HEAD
	remoteDefaultBranchRgx = regexp.MustCompile(`^ref:\s+([^\s]+)\s+HEAD`)
)

func init() {
	gitExecutablePath = exec.Command("git").String()
}

type gcMode string

const (
	gcAuto       = "auto"
	gcAlways     = "always"
	gcAggressive = "aggressive"
	gcOff        = "off"
)

// Repository represents the mirrored repository of the given remote.
// The implementation borrows heavily from https://github.com/kubernetes/git-sync.
// A Repository is safe for concurrent use by multiple goroutines.
type Repository struct {
	lock          lock.RWMutex             // repository will be locked during mirror
	gitURL        *giturl.URL              // parsed remote git URL
	remote        string                   // remote repo to mirror
	root          string                   // absolute path to the root where repo directory createdabsolute path to the root where repo directory created
	dir           string                   // absolute path to the repo directory
	interval      time.Duration            // how long to wait between mirrors
	mirrorTimeout time.Duration            // the total time allowed for the mirror loop
	auth          *Auth                    // auth information including ssh key path
	gitGC         gcMode                   // garbage collection
	envs          []string                 // envs which will be passed to git commands
	running       bool                     // indicates if repository is running the mirror loop
	workTreeLinks map[string]*WorkTreeLink // list of worktrees which will be maintained
	stop, stopped chan bool                // chans to stop mirror loops
	log           *slog.Logger
}

// NewRepository creates new repository from the given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called.
func NewRepository(repoConf RepositoryConfig, envs []string, log *slog.Logger) (*Repository, error) {
	remoteURL := giturl.NormaliseURL(repoConf.Remote)

	gURL, err := giturl.Parse(remoteURL)
	if err != nil {
		return nil, err
	}

	if log == nil {
		log = slog.Default()
	}

	log = log.With("repo", gURL.Repo)

	if !filepath.IsAbs(repoConf.Root) {
		return nil, fmt.Errorf("repository root '%s' must be absolute", repoConf.Root)
	}

	if repoConf.Interval < minAllowedInterval {
		return nil, fmt.Errorf("provided interval between mirroring is too sort (%s), must be > %s", repoConf.Interval, minAllowedInterval)
	}

	switch repoConf.GitGC {
	case gcAuto, gcAlways, gcAggressive, gcOff:
	default:
		return nil, fmt.Errorf("wrong gc value provided, must be one of %s, %s, %s, %s",
			gcAuto, gcAlways, gcAggressive, gcOff)
	}

	// we are going to create bare repo which caller cannot use directly
	// hence we can add repo dir (with .git suffix to indicate bare repo) to the provided root.
	// this also makes it safe to delete this dir and re-create it if needed
	// also this root could have been shared with other mirror repository (repoPool)
	repoDir := gURL.Repo
	if !strings.HasSuffix(repoDir, ".git") {
		repoDir += ".git"
	}
	repoDir = filepath.Join(repoConf.Root, repoDir)

	repo := &Repository{
		gitURL:        gURL,
		remote:        remoteURL,
		root:          repoConf.Root,
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
	}

	for _, wtc := range repoConf.Worktrees {
		if err := repo.AddWorktreeLink(wtc.Link, wtc.Ref, wtc.Ref); err != nil {
			return nil, fmt.Errorf("unable to create worktree link err:%w", err)
		}
	}
	return repo, nil
}

// AddWorktreeLink adds add workTree link to the mirror repository.
func (r *Repository) AddWorktreeLink(link, ref, pathspec string) error {
	if link == "" {
		return fmt.Errorf("symlink path cannot be empty")
	}

	if v, ok := r.workTreeLinks[link]; ok {
		return fmt.Errorf("worktree with given link already exits link:%s ref:%s", v.link, v.ref)
	}

	linkAbs := absLink(r.root, link)

	if ref == "" {
		ref = "HEAD"
	}

	_, linkFile := splitAbs(link)

	wt := &WorkTreeLink{
		name:     linkFile,
		link:     linkAbs,
		ref:      ref,
		pathspec: pathspec,
		log:      r.log.With("worktree", linkFile),
	}

	r.workTreeLinks[link] = wt
	return nil
}

// Hash returns the hash of the given revision and for the path if specified.
func (r *Repository) Hash(ctx context.Context, ref, path string) (string, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.hash(ctx, ref, path)
}

// LogMsg returns the formatted log subject with author info of the given
// revision and for the path if specified.
func (r *Repository) LogMsg(ctx context.Context, ref, path string) (string, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	args := []string{"log", `--pretty=format:'%s (%an)'`, "-n", "1", ref}
	if path != "" {
		args = append(args, "--", path)
	}
	// git log --pretty=format:'%s (%an)' -n 1 <ref> [-- <path>]
	msg, err := runGitCommand(ctx, r.log, r.envs, r.dir, args...)
	if err != nil {
		return "", err
	}
	return strings.Trim(msg, "'"), nil
}

// Subject returns commit subject of given commit hash
func (r *Repository) Subject(ctx context.Context, hash string) (string, error) {
	if err := r.ObjectExists(ctx, hash); err != nil {
		return "", err
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	args := []string{"show", `--no-patch`, `--format='%s'`, hash}
	msg, err := runGitCommand(ctx, r.log, r.envs, r.dir, args...)
	if err != nil {
		return "", err
	}
	return strings.Trim(msg, "'"), nil
}

// ChangedFiles returns path of the changed files for given commit hash
func (r *Repository) ChangedFiles(ctx context.Context, hash string) ([]string, error) {
	if err := r.ObjectExists(ctx, hash); err != nil {
		return nil, err
	}

	r.lock.RLock()
	defer r.lock.RUnlock()

	args := []string{"show", `--name-only`, `--pretty=format:`, hash}
	msg, err := runGitCommand(ctx, r.log, r.envs, r.dir, args...)
	if err != nil {
		return nil, err
	}
	return strings.Split(msg, "\n"), nil
}

// ObjectExists returns error is given object is not valid or if it doesn't exists
func (r *Repository) ObjectExists(ctx context.Context, obj string) error {
	r.lock.RLock()
	defer r.lock.RUnlock()

	args := []string{"cat-file", `-e`, obj}
	_, err := runGitCommand(ctx, r.log, r.envs, r.dir, args...)
	return err
}

// Clone creates a single-branch local clone of the mirrored repository to a new location on
// disk. On success, it returns the hash of the new repository clone's HEAD.
// if pathspec is provided only those paths will be checked out.
// if ref is commit hash then pathspec will be ignored.
// if rmGitDir is true `.git` folder will be deleted after the clone.
// if dst not empty all its contents will be removed.
func (r *Repository) Clone(ctx context.Context, dst, ref, pathspec string, rmGitDir bool) (string, error) {
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

	r.lock.RLock()
	defer r.lock.RUnlock()

	if IsCommitHash(ref) {
		return r.cloneByRef(ctx, dst, ref, pathspec, rmGitDir)
	}
	return r.cloneByBranch(ctx, dst, ref, pathspec, rmGitDir)
}

func (r *Repository) cloneByBranch(ctx context.Context, dst, branch, pathspec string, rmGitDir bool) (string, error) {
	args := []string{"clone", "--no-checkout", "--single-branch"}
	if branch != "HEAD" {
		args = append(args, "-b", branch)
	}
	args = append(args, r.dir, dst)
	// git clone --no-checkout --single-branch [-b <branch>] <remote> <dst>
	if _, err := runGitCommand(ctx, r.log, nil, "", args...); err != nil {
		return "", err
	}

	args = []string{"checkout", branch}
	if pathspec != "" {
		args = append(args, "--", pathspec)
	}
	// git checkout <branch> -- <pathspec>
	if _, err := runGitCommand(ctx, r.log, nil, dst, args...); err != nil {
		return "", err
	}

	// get the hash of the repos HEAD
	args = []string{"log", "--pretty=format:%H", "-n", "1", "HEAD"}
	if pathspec != "" {
		args = append(args, "--", pathspec)
	}
	// git log --pretty=format:%H -n 1 HEAD [-- <path>]
	hash, err := runGitCommand(ctx, r.log, nil, dst, args...)
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

func (r *Repository) cloneByRef(ctx context.Context, dst, ref, pathspec string, rmGitDir bool) (string, error) {
	// git clone --no-checkout <remote> <dst>
	if _, err := runGitCommand(ctx, r.log, nil, "", "clone", "--no-checkout", r.dir, dst); err != nil {
		return "", err
	}

	args := []string{"reset", "--hard", ref}
	// git reset --hard <ref>
	if out, err := runGitCommand(ctx, r.log, nil, dst, args...); err != nil {
		return "", err
	} else {
		fmt.Println(out)
	}

	// get the hash of the repos HEAD
	args = []string{"log", "--pretty=format:%H", "-n", "1", "HEAD"}
	if pathspec != "" {
		args = append(args, "--", pathspec)
	}

	// git log --pretty=format:%H -n 1 HEAD [-- <path>]
	hash, err := runGitCommand(ctx, r.log, nil, dst, args...)
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

// StartLoop mirrors repository periodically based on repo's mirror interval
func (r *Repository) StartLoop(ctx context.Context) {
	if r.running {
		r.log.Error("mirror loop has already been started")
		return
	}
	r.running = true
	r.log.Info("started repository mirror loop", "interval", r.interval)

	defer func() {
		r.running = false
		close(r.stopped)
	}()

	for {
		// to stop mirror running indefinitely we will use time-out
		mCtx, cancel := context.WithTimeout(ctx, r.mirrorTimeout)
		err := r.Mirror(mCtx)
		cancel()
		if err != nil {
			r.log.Error("repository mirror failed", "err", err)
		}
		recordGitMirror(r.gitURL.Repo, err == nil)

		t := time.NewTimer(r.interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		}
	}
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
		return fmt.Errorf("unable to init repo:%s  err:%w", r.gitURL.Repo, err)
	}

	refs, err := r.fetch(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch repo:%s  err:%w", r.gitURL.Repo, err)
	}

	fetchTime := time.Since(start)

	// worktree might need re-creating if it fails check
	// so always ensure worktree even if nothing fetched
	for _, wl := range r.workTreeLinks {
		if err := r.ensureWorktreeLink(ctx, wl); err != nil {
			return fmt.Errorf("unable to ensure worktree links repo:%s link:%s  err:%w", r.gitURL.Repo, wl.name, err)
		}
	}

	// clean-up can be skipped
	if len(refs) == 0 {
		return nil
	}

	if err := r.cleanup(ctx); err != nil {
		return fmt.Errorf("unable to cleanup repo:%s  err:%w", r.gitURL.Repo, err)
	}

	r.log.Info("mirror cycle complete", "time", time.Since(start), "fetch-time", fetchTime, "updated-refs", len(refs))
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
			if err := reCreate(r.dir); err != nil {
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
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "init", "-q", "--bare"); err != nil {
		return fmt.Errorf("unable to init repo err:%w", err)
	}

	// create new remote "origin"
	// The "origin" remote has special meaning, like in relative-path submodules.
	// use --mirror=fetch as we want to create mirrored bare repository. it will make sure
	// everything in refs/* on the remote will be directly mirrored into refs/* in the local repository.
	// git remote add --mirror=fetch origin <remote>
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "remote", "add", "--mirror=fetch", "origin", r.remote); err != nil {
		return fmt.Errorf("unable to set remote err:%w", err)
	}

	// get default branch from remote and set it as local HEAD
	headBranch, err := r.getRemoteDefaultBranch(ctx)
	if err != nil {
		return fmt.Errorf("unable to get remote default branch err:%w", err)
	}

	// set local HEAD to remote HEAD/default branch
	// git symbolic-ref HEAD <headBranch>(refs/heads/master)
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "symbolic-ref", "HEAD", headBranch); err != nil {
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
	envs := []string{}
	if giturl.IsSCPURL(r.remote) || giturl.IsSSHURL(r.remote) {
		envs = append(envs, r.auth.gitSSHCommand())
	}

	// git ls-remote --symref origin HEAD
	out, err := runGitCommand(ctx, r.log, envs, r.dir, "ls-remote", "--symref", "origin", "HEAD")
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
	if ok, err := runGitCommand(ctx, r.log, r.envs, r.dir, "rev-parse", "--is-bare-repository"); err != nil {
		r.log.Error("unable to verify bare repo", "path", r.dir, "err", err)
		return false
	} else if ok != "true" {
		r.log.Error("repo is not a bare repository", "path", r.dir)
		return false
	}

	// Check that this is actually the root of the repo.
	// git rev-parse --absolute-git-dir
	if root, err := runGitCommand(ctx, r.log, r.envs, r.dir, "rev-parse", "--absolute-git-dir"); err != nil {
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
	if stdout, err := runGitCommand(ctx, r.log, r.envs, r.dir, "config", "--get", "remote.origin.url"); err != nil {
		r.log.Error("can't get repo config remote.origin.url", "path", r.dir, "err", err)
		return false
	} else if stdout != r.remote {
		r.log.Error("repo configured with diff remote url", "path", r.dir, "remote.origin.url", stdout)
		return false
	}

	// verify origin's fetch refspec
	// git config --get remote.origin.fetch
	if stdout, err := runGitCommand(ctx, r.log, r.envs, r.dir, "config", "--get", "remote.origin.fetch"); err != nil {
		r.log.Error("can't get repo config remote.origin.fetch", "path", r.dir, "err", err)
		return false
	} else if stdout != defaultRefSpec {
		r.log.Error("repo configured with incorrect fetch refspec", "path", r.dir, "remote.origin.fetch", stdout)
		return false
	}

	// Consistency-check the repo.  Don't use --verbose because it can be
	// REALLY verbose.
	// git fsck --no-progress --connectivity-only
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "fsck", "--no-progress", "--connectivity-only"); err != nil {
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

	envs := []string{}
	if giturl.IsSCPURL(r.remote) || giturl.IsSSHURL(r.remote) {
		envs = append(envs, r.auth.gitSSHCommand())
	}

	// git fetch origin --prune --no-progress --no-auto-gc
	out, err := runGitCommand(ctx, r.log, envs, r.dir, args...)
	return updatedRefs(out), err
}

// hash returns the hash of the given revision and for the path if specified.
func (r *Repository) hash(ctx context.Context, ref, path string) (string, error) {
	args := []string{"log", "--pretty=format:%H", "-n", "1", ref}
	if path != "" {
		args = append(args, "--", path)
	}
	// git log --pretty=format:%H -n 1 <ref> [-- <path>]
	return runGitCommand(ctx, r.log, r.envs, r.dir, args...)
}

// ensureWorktreeLink will create / validate worktrees
// it will remove worktree if tracking ref is removed from the remote
func (r *Repository) ensureWorktreeLink(ctx context.Context, wl *WorkTreeLink) error {
	// get remote hash from mirrored repo for the worktree link
	remoteHash, err := r.hash(ctx, wl.ref, wl.pathspec)
	if err != nil {
		return fmt.Errorf("unable to get hash for worktree:%s err:%w", wl.name, err)
	}
	var currentHash, currentPath string

	// we do not care if we cant get old worktree path as we can create it
	currentPath, err = wl.currentWorktree()
	if err != nil {
		// in case of error we create new worktree
		wl.log.Error("unable to get current worktree path", "err", err)
	}

	if currentPath != "" {
		// get hash from the worktree folder
		currentHash, err = wl.workTreeHash(ctx, currentPath)
		if err != nil {
			// in case of error we create new worktree
			wl.log.Error("unable to get current worktree hash", "err", err)
		}
	}

	// we got empty remote hash so either given worktree ref do not exits yet or
	// its removed from the remote
	if remoteHash == "" {
		wt, err := wl.currentWorktree()
		if err != nil {
			wl.log.Error("can't get current worktree", "err", err)
			return nil
		}
		if wt == "" {
			return nil
		}

		wl.log.Info("remote hash is empty, removing old worktree", "path", currentPath)
		if err := r.removeWorktree(ctx, wt); err != nil {
			wl.log.Error("unable to remove old worktree", "err", err)
		}

		return nil
	}

	if currentHash == remoteHash {
		if wl.sanityCheckWorktree(ctx) {
			wl.log.Debug("current hash is same as remote and checks passed", "hash", currentHash)
			return nil
		}
		wl.log.Error("worktree failed checks, re-creating...", "path", currentPath)
	}

	wl.log.Info("worktree update required", "remoteHash", remoteHash, "currentHash", currentHash)
	newPath, err := r.createWorktree(ctx, wl, remoteHash)
	if err != nil {
		return fmt.Errorf("unable to create worktree for '%s' err:%w", wl.name, err)
	}

	if err = publishSymlink(wl.link, newPath); err != nil {
		return fmt.Errorf("unable to publish symlink err:%w", err)
	}

	// since we use hash to create worktree path it is possible that we
	// may have re-created current worktree
	if currentPath != "" && currentPath != newPath {
		if err := r.removeWorktree(ctx, currentPath); err != nil {
			wl.log.Error("unable to remove old worktree", "err", err)
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
	if err := r.removeWorktree(ctx, wtPath); err != nil {
		return wtPath, err
	}

	wl.log.Info("creating worktree", "path", wtPath, "hash", hash)
	// git worktree add --force --detach --no-checkout <wt-path> <hash>
	_, err := runGitCommand(ctx, wl.log, nil, r.dir, "worktree", "add", "--force", "--detach", "--no-checkout", wtPath, hash)
	if err != nil {
		return wtPath, err
	}

	// only checkout required path if specified
	args := []string{"checkout", hash}
	if wl.pathspec != "" {
		args = append(args, "--", wl.pathspec)
	}
	// git checkout <hash> -- <pathspec>
	if _, err := runGitCommand(ctx, wl.log, nil, wtPath, args...); err != nil {
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
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "worktree", "prune", "--verbose"); err != nil {
		return err
	}
	return nil
}

// cleanup removes old worktrees and runs git's garbage collection.
func (r *Repository) cleanup(ctx context.Context) error {
	var cleanupErrs []error

	// Clean up previous worktree(s).
	if _, err := r.removeStaleWorktrees(); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	// Let git know we don't need those old commits any more.
	// git worktree prune -v
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "worktree", "prune", "--verbose"); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	// Expire old refs.
	// git reflog expire --expire-unreachable=all --all
	if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, "reflog", "expire", "--expire-unreachable=all", "--all"); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	// Run GC if needed.
	if r.gitGC != gcOff {
		args := []string{"gc"}
		switch r.gitGC {
		case gcAuto:
			args = append(args, "--auto")
		case gcAlways:
			// no extra flags
		case gcAggressive:
			args = append(args, "--aggressive")
		}
		if _, err := runGitCommand(ctx, r.log, r.envs, r.dir, args...); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}

	if len(cleanupErrs) > 0 {
		return fmt.Errorf("%s", cleanupErrs)
	}
	return nil
}

func (r *Repository) removeStaleWorktrees() (int, error) {
	var currentWTDirs []string

	for _, wt := range r.workTreeLinks {
		t, err := wt.currentWorktree()
		if err != nil {
			r.log.Error("unable to read worktree link", "worktree", wt.name, "err", err)
			continue
		}
		if t != "" {
			_, wtDir := splitAbs(t)
			currentWTDirs = append(currentWTDirs, wtDir)
		}
	}

	count := 0
	err := removeDirContentsIf(r.worktreesRoot(), r.log, func(fi os.FileInfo) (bool, error) {
		// delete files that are over the stale time out, and make sure to never delete the current worktree
		if !slices.Contains(currentWTDirs, fi.Name()) && time.Since(fi.ModTime()) > staleTimeout {
			count++
			r.log.Info("removing stale worktree", "worktree", fi.Name())
			return true, nil
		}
		return false, nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return count, nil
}
