# git-mirror

git-mirror is an app to periodically mirror (bare clones) remote repositories locally.
It supports multiple mirrored checked out worktrees on different references
and it can also mirror multiple repositories.

`git-mirror` can be used as golang library in your code. [docs](https://pkg.go.dev/github.com/utilitywarehouse/git-mirror/pkg/mirror)  
The mirror is created with `--mirror=fetch` hence everything in `refs/*` on the remote
will be directly mirrored into `refs/*` in the local repository.

The implementation borrows heavily from [kubernetes/git-sync](https://github.com/kubernetes/git-sync).
If you want to sync single repository on one reference then you are probably better off
with [kubernetes/git-sync](https://github.com/kubernetes/git-sync), as it provides
a lot more customisation. 
`git-mirror` should be used if multiple mirrored repositories with multiple checked out branches (worktrees) is required.

## Usage

```
Usage:
    git-mirror [global options]

GLOBAL OPTIONS:
    -log-level value          (default: 'info') Log level [$LOG_LEVEL]
    -config value             (default: '/etc/git-mirror/config.yaml') Absolute path to the config file. [$GIT_MIRROR_CONFIG]
    -watch-config value       (default: true) watch config for changes and reload when changes encountered. [$GIT_MIRROR_WATCH_CONFIG]
    -http-bind-address value  (default: ':9001') The address the web server binds to. [$GIT_MIRROR_HTTP_BIND]
```

## Config
configuration file contains `default` parameters and list of repositories.
each repository also contains list of worktrees to checkout.
`defaults` fields values are used if repository parameters are not specified.

```yaml
defaults:
  # root is the absolute path to the root dir where all repositories directories
  # will be created all repos worktree links will be created here if not
  # specified in repo config (default: '/tmp/git-mirror')
  root: /tmp/git-mirror

  # link_root is the absolute path to the dir which is the root for the worktree links
	# if link is a relative path it will be relative to link_root dir
  # if link is not specified it will be constructed from repo name and worktree ref
	# and it will be placed in this dir
	# if not specified it will be same as root
  link_root: /app/links
  
  # interval is time duration for how long to wait between mirrors. (default: '30s')
  interval: 30s

  # mirrorTimeout represents the total time allowed for the complete mirror loop (default: '2m')
  mirror_timeout: 2m

  # git_gc garbage collection string. valid values are
  # 'auto', 'always', 'aggressive' or 'off' (default: 'always')
  git_gc: always

  # auth config to fetch remote repos
  auth:
    # path to the ssh key & known hosts used to fetch remote
    ssh_key_path: /etc/git-secret/ssh
    ssh_known_hosts_path: /etc/git-secret/known_hosts
repositories:
    # remote is the git URL of the remote repository to mirror.
    # supported urls are 'git@host.xz:org/repo.git','ssh://git@host.xz/org/repo.git'
    # or 'https://host.xz/org/repo.git'. '.git' suffix is optional
  - remote: https://github.com/utilitywarehouse/git-mirror # required

    # following fields are optional.
    # if these fields are not specified values from defaults section will be used
    root: /some/other/location
    link_root: /some/path
    interval: 1m
    mirror_timeout: 5m
    git_gc: always
    auth:
      ssh_key_path: /some/other/location
      ssh_known_hosts_path: /some/other/location
    worktrees:
      # link is the path at which to create a symlink to the worktree dir
      # if path is not absolute it will be created under repository link_root
      # if link is not specified it will be constructed from repo name and worktree ref
      # and it will be placed in link_root dir
      - link: alerts

        # ref represents the git reference of the worktree branch, tags or hash
        # are supported. default is HEAD
        ref: main

        # pathspecs is the pattern used to checkout paths in Git commands.
        # its optional, if omitted whole repo will be checked out
        pathspecs: 
          - path
          - path2/*.yaml
```
For more details about `pathspecs`, see [git glossary](https://git-scm.com/docs/gitglossary#Documentation/gitglossary.txt-aiddefpathspecapathspec)

App can load changes in config without restart. At repository level only 
adding and removing repository is supported. changes in interval, timeout 
and auth will require an app restart. 
At worktree level apart from adding or removing, changes in existing worktree's
link, ref and pathspecs is supported.