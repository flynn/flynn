---
title: Scary How Much We Fixed Last Week!
date: November 3, 2014
---

We made another major docs push to Flynn this week. We've started our *How to Deploy* series with the most-requested languages from the community.

We also pushed a major feature (custom builds) and solved some annoying bugs–PHP support is fixed! As always, we are continuing to close issues and make Flynn more stable.

## Changes

### New Documentation

We've launched a series of **Language Guides** to help you deploy your favorite language to Flynn:

* [Go](https://flynn.io/docs/how-to-deploy-go)
* [Java](https://flynn.io/docs/how-to-deploy-java)
* [Node.js](https://flynn.io/docs/how-to-deploy-nodejs)
* [PHP](https://flynn.io/docs/how-to-deploy-php)
* [Python](https://flynn.io/docs/how-to-deploy-python)
* [Ruby](https://flynn.io/docs/how-to-deploy-ruby)


### Notable Enhancements

* **We've added support for [releasing custom builds](https://github.com/flynn/flynn/pull/382)**. This allows you to customize Flynn and test it out on your servers without merging your changes upstream first. We've expanded our [Development Documentation](https://flynn.io/docs/development#releasing-flynn) to walk you through these improvements.
* **We [re-wrote the log handler](https://github.com/flynn/flynn/pull/163)** to improve reliability.
* **We updated to Go 1.4beta1** to improve reliability and security. This may also improve performance as development continues.
 
### Major Bugs Fixed

* Fixed PHP support ([#360](https://github.com/flynn/flynn/pull/360))
* Fixed log decoding crash ([#92](https://github.com/flynn/flynn/issues/92))
* Fixed build dependency tracking ([#374](https://github.com/flynn/flynn/pull/374))

## What's Next

Flynn is moving towards production stability at a consistent pace. We continue to work hard on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Email us](mailto:contact@flynn.io) whatever is on your mind!
