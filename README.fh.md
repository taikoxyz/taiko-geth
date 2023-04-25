## StreamingFast Firehose Fork of `Ethereum` (`geth` client)

This is our Firehose instrumented fork of [ethereum/go-ethereum](https://github.com/ethereum/go-ethereum) repository. In this README, you will find instructions about how to work with this repository.

### Versions

The Firehose instrumentation have a major version for the messages exchanges with Firehose on Ethereum binary (`fheth`). The
versions we currently supported:

- Version 2 using the `firehose-v2` branch and `fh2` tag(s) suffix

> **Warning** Version 1 using the `firehose-v1` branch and `fh1` tag(s) suffix is not supported anymore, branches and tags exist for historical access.

Read [Branches & Workflow](#branches-&-workflow) section for more details about how we handle branching model and versions.

### Initialization

The tooling and other instructions expect the following project
structure, it's easier to work with the Firehose fork when you use
the same names and settings.

```
cd ~/work
git clone --branch="firehose-v1" git@github.com:streamingfast/go-ethereum.git
cd go-ethereum

git remote rename origin sf

git checkout firehose-v2
git checkout firehose-v2-v1.10.1

git remote add origin https://github.com/ethereum/go-ethereum.git
git remote add polygon https://github.com/maticnetwork/bor.git
git remote add bsc https://github.com/binance-chain/bsc.git

git fetch origin
git fetch polygon
git fetch bsc

git checkout release/geth-1.11.x-fh2
git checkout release/bsc-1.1.x-fh2
git checkout release/polygon-0.x-fh2
```

If you want to work on one of the unmaintained branches:

```
git remote add heco https://github.com/HuobiGroup/huobi-eco-chain.git
git remote add optimism https://github.com/ethereum-optimism/go-ethereum.git
git remote add fantom-geth https://github.com/Fantom-foundation/go-ethereum.git
git fetch heco
git fetch optimism
git fetch fantom-geth
git checkout release/heco-1.0.x-fh1
git checkout release/optimism-0.1.x-fh1
git checkout release/fantom-geth-1.9.x-fh1
```

> **Warning** Those are unmaintained since a long time and have no `fh2` version, so bringing in `fh2` changes is the first step that would be required.

##### Assumptions

For the best result when working with this repository and the scripts it contains:

- The remote `sf` exists on main module and points to `git@github.com:streamingfast/go-ethereum.git`
- The remote `origin` exists on main module and points to https://github.com/ethereum/go-ethereum.git

### Branches & Workflow

Dealing with a big repository like Ethereum that have multiple versions for which we need
to track multiple forks (`Matic`, `BSC`) pose a branch management challenges.

Even more that we have our own set of patches to enable deep data extraction
for Firehose consumption.

We use merging of the branches into one another to make that work manageable.
The first and foremost important rule is that we always put new development
in the `firehose-v2` branch.

This branch must always be tracking the lowest supported version of all. Indeed,
this is our "work" branch for our patches, **new development must go there**. If you
perform our work with newer code, the problem that will arise is that this new
firehose work will not be mergeable into forks or older release that we still
support!

We also support and intermediate branch for version v1.10.1 of Geth. The reason for this
is that this version added refactoring to support multiple transactions types which needs to be
properly handled (1.10.0 more specifically did the refactoring, but 1.10.1 was added almost at
the same moment as 1.10.0).

`firehose-v2` should be merged always in `firehose-v2-v1.10.1`.

We then create `release/<identifier>` branch that tracks the version of interest
for us, versions that we will manages and deploy.

Currently supported forks & version and the release branch

- `firehose-v2` - Default branch with all Firehose commits in it, based on Geth `1.9.10` (Ethereum Block Version 2).
- [release/geth-1.11.x-fh2](https://github.com/streamingfast/go-ethereum/tree/release/geth-1.11.x-fh2) - Ethereum Geth, latest update for this branch is `1.11.3` ([https://github.com/ethereum/go-ethereum](https://github.com/ethereum/go-ethereum)).
- [release/polygon-0.x-fh2](https://github.com/streamingfast/go-ethereum/tree/release/polygon-0.x-fh2) - Polygon fork (a.k.a Matic), based on Geth `1.10.3`, latest update for this branch is `v0.3.2` ([https://github.com/maticnetwork/bor](https://github.com/maticnetwork/bor)).
- [release/bsc-1.1.x-fh2](https://github.com/streamingfast/go-ethereum/tree/release/bsc-1.1.x-fh2) - BSC fork (Binance), based on Geth `1.10.22`, latest update for this branch is `v1.1.18` ([https://github.com/binance-chain/bsc](https://github.com/binance-chain/bsc)).

> **Note** To find on which Geth version a particular fork is, you can do `git merge-base sf/release/heco-v1.0.x-fh1 origin/master` where `origin/master` is the `master` branch of the original Geth repository (https://github.com/ethereum/go-ethereum).

#### Unsupported

- [release/heco-1.0.x-fh1](https://github.com/streamingfast/go-ethereum/tree/release/heco-1.0.x-fh1) - HECO fork (a.k.a Huobi Eco Chain), based on Geth `1.10.1`, latest update for this branch is `v1.1.1` ([https://github.com/HuobiGroup/huobi-eco-chain](https://github.com/HuobiGroup/huobi-eco-chain)).
- [release/optimism-0.1.x-fh1](https://github.com/streamingfast/go-ethereum/tree/release/optimism-0.1.x-fh1) - Optimism fork, based on Geth `1.9.10`, latest update for this branch is `v0.1.4` ([https://github.com/ethereum-optimism/go-ethereum](https://github.com/ethereum-optimism/go-ethereum)).
- [release/fantom-geth-1.9.x-fh1](https://github.com/streamingfast/go-ethereum/tree/release/fantom-geth-1.9.x-fh1) - Fantom Geth fork, based on Geth `1.9.22`, latest update for this branch is `v1.9.22-ftm-0.5` (a branch) ([https://github.com/Fantom-foundation/go-ethereum](https://github.com/Fantom-foundation/go-ethereum)).

#### Making New Firehose Changes

Making new changes should be performed on the `firehose-v2-v1.10.1` branch. When happy
with the changes, simply merge the `firehose-v2-v1.10.1` branch in all the release branches we track
and support.

    git checkout firehose-v2-v1.10.1
    git pull -p

    # Perform necessary changes, tests and commit(s)

    git checkout release/geth-1.10.x-fh2
    git pull -p
    git merge firehose-v2-v1.10.1

    git checkout release/polygon-0.x-fh2
    git pull -p
    git merge firehose-v2-v1.10.1

    git push sf firehose-v2-v1.10.1 release/geth-1.10.x-fh2 release/polygon-0.x-fh2

**Note** On newer fork, you must merge the right version, for example if the fork never merged 1.10.1 from the upstream Geth, you must **not** use
`firehose-v2-v1.10.1` but instead use `firehose-v2` which is based on 1.9.10.

### Update to New Upstream Version

We assume you are in the top directory of the repository when performing the following
operations. Here, we outline the rough idea. Extra details and command lines to use
will be completed later if missing.

We are using `v1.10.18` as the example release tag that we want to update to, assuming
`v1.10.17` was the previous latest merged tag. Change
those with your own values.

First step is to checkout the release branch of the series you are currently
updating to:

    git checkout release/geth-1.11.x-fh2
    git pull -p

You first fetch the origin repository new data from Git:

    git fetch origin -p

Then apply the update

    git merge v1.11.4

Solve conflicts if any. Once all conflicts have been resolved, commit then
create a tag with release

    git tag geth-v1.11.4-fh2

Then push all that to the repository:

    git push sf release/geth-1.11.x-fh2 geth-v1.11.4-fh2

### Development

All the *new* development should happen in the `firehose-v2` branch, this is our own branch
containing our commits.

##### Build Locally

    go install ./cmd/geth

#### Release

   Github actions are automatically created when creating a tag

### View only our commits

**Important** To correctly work, you need to use the right base branch, otherwise, it will be screwed up. The `firehose-v2`
branch was based on `v1.9.23` at time of writing.

* From `gitk`: `gitk --no-merges --first-parent v1.9.23..firehose-v2`
* From terminal: `git log --decorate --pretty=oneline --abbrev-commit --no-merges --first-parent v1.9.23..firehose-v2`
* From `GitHub`: [https://github.com/streamingfast/go-ethereum/compare/v1.9.23...firehose-v1](https://github.com/streamingfast/go-ethereum/compare/v1.9.23...firehose-v2)
* Modified files in our fork: `git diff --name-status v1.9.23..firehose-v2 | grep -E "^M" | cut -d $'\t' -f 2`
