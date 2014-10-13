---
title: Contributing
layout: docs
---

# Contribution Guide

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. We will not accept Pull requests that do not address these priorities until Flynn is production ready.

Please read these Contribution Guidelines and our [Project Roadmap](/roadmap.html) before contributing.

There are many ways to help Flynn besides contributing code:

 - [Fix bugs or file issues](https://github.com/flynn/flynn/issues)
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website

## Contributing Code

Unless you are fixing a known bug, we **strongly** recommend discussing it with the core team via a [GitHub issue](https://github.com/flynn/flynn/issues), [IRC](irc://irc.freenode.net/flynn), or [email](mailto:contact@flynn.io) before getting started to ensure your work is consistent with Flynn's roadmap and architecture.

You should make contributions via pull request. Note that **all patches from all contributors get reviewed**. After a pull request is made, other contributors will offer feedback. If the patch passes review, a maintainer will accept it with a comment. If the pull request fails testing, authors should update their request to address the failures until all tests pass and the pull request can be merged.

All patches require at least one review from a maintainer (even patches from other maintainers). If the MAINTAINERS file only lists one maintainer, then review is not required for patches from the sole maintainer (but it is encouraged).

To be a maintainer, you must have a reliable set of recent contributions to the feature you are working on. Repositories with only two maintainers can go stale quickly due to the review requirements. Only active contributors can remain maintainers. Any change in maintainers should not be taken personally. Our only concern is for the health and organization of the code and project.

## Developer’s Certificate of Origin

All contributions must include acceptance of the DCO:

```text
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
660 York Street, Suite 102,
San Francisco, CA 94110 USA

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

To accept the DCO, simply add this line to each commit message with your name and email address (`git commit -s` will do this for you):

```text
Signed-off-by: Jane Example <jane@example.com>
```

For legal reasons, no anonymous or pseudonymous contributions are accepted ([contact us](mailto:contact@flynn.io) if this is an issue).

## Pull request procedure

To make a pull request, you will need a Github account. For help creating a pull request, see Github's documentation on [forking](https://help.github.com/articles/fork-a-repo) and [pull requests](https://help.github.com/articles/using-pull-requests). Pull requests should target the `master` branch. Before creating a pull request, go through this checklist:

1. Create a feature branch off of `master` so that changes do not get mixed up.
1. [Rebase](http://git-scm.com/book/en/Git-Branching-Rebasing) your local changes against the `master` branch.
1. Run the full project test suite with the `go test ./...` (or equivalent) command and confirm that it passes.
1. Run `gofmt -s` (if the project is written in Go).
1. Accept the Developer's Certificate of Origin on all commits (see above).

Pull requests are treated as "review requests," and a maintainer will give feedback on the style and content of the patch.

Unless making a change to documentation, all pull requests must include tests that test your change. If you believe a change will be difficult to test for, please include a note in your commit message explaining why.

## Communication

We use the #flynn IRC channel on [Freenode](irc://chat.freenode.net/flynn). You are welcome to drop in and ask questions, discuss bugs, etc. It is [logged on BotBot.me](https://botbot.me/freenode/flynn/) and you can [connect using webchat](https://webchat.freenode.net/?channels=flynn) if you don't have an IRC client.

## Conduct

Whether you are a regular contributor or a newcomer, we care about making this community a safe place for you and we've got your back.

* We are committed to providing a friendly, safe and welcoming environment for all, regardless of gender, sexual orientation, disability, ethnicity, religion, or similar personal characteristic.
* Please avoid using nicknames that might detract from a friendly, safe and welcoming environment for all.
* Be kind and courteous. There is no need to be mean or rude.
* We will exclude you from interaction if you insult, demean or harass anyone. In particular, we do not tolerate behavior that excludes people in socially marginalized groups.
* Private harassment is also unacceptable. No matter who you are, if you feel you are being harassed or made uncomfortable by a community member, please contact one of the channel ops or a member of the Flynn core team immediately.
* Likewise any spamming, trolling, flaming, baiting or other attention-stealing behavior is not welcome.

We welcome discussion about creating a welcoming, safe, and productive environment for the community. If you have any questions, feedback, or concerns [please let us know](mailto:contact@flynn.io).
