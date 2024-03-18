git-mirror is an cli tool that periodically mirrors (bare clones) remote repositories locally.
The mirror is created with `--mirror=fetch` hence everything in `refs/*` on the remote
will be directly mirrored into `refs/*` in the local repository. 
it can also maintain multiple mirrored checked out worktrees on different references.
`git-mirror` is also available as public pkg to use in your own golang code.


The implementation borrows heavily from [kubernetes/git-sync](https://github.com/kubernetes/git-sync).
If you want to sync single repository on one reference then you are probably better off
with [kubernetes/git-sync](https://github.com/kubernetes/git-sync), as it provides
a lot more customisation. 
`git-mirror` should be used if multiple mirrored repositories with multiple checked out branches (worktrees) is required.



## Usage

cli is still WIP


