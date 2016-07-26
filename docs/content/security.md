---
title: Security
layout: docs
toc_min_level: 2
---

# Security

If you have an issue to report, please jump to the [reporting
issues](#reporting-issues) section.

Security is extraordinarily important to us.

Because Flynn is an integrated platform that we control end-to-end we are able
to implement many best practices by default and deploy technologies that would
be extremely difficult for users to take advantage of on their own.

Now that we've reached the point where Flynn is stable enough for the majority
of users in production, we are focusing on making Flynn secure by default.

Until we reach the point where everything is secure by default, it's important
to understand what the current security properties of Flynn are.

## Distribution Security

All binaries that we provide including `flynn-host`, the `flynn` CLI tool, and
container images are distributed securely using the [The Update
Framework](http://theupdateframework.com). TUF includes a robust, role-based
signature system and protects against many attacks including downgrades and CDN
compromise. In addition to TUF, we serve all content exclusively over HTTPS.

Our Vagrant virtual machine images are served over HTTPS but are not currently
signed, as signatures are not supported by Vagrant.

## Internal Communication

Flynn uses several ports to communicate internally, and currently there is no
authentication system for internal communication, so access to these ports must
not be exposed to the Internet. A firewall must be configured so that the only
Flynn ports accessible are 80 and 443 to prevent compromise. Access to these
internal Flynn ports is equivalent to root access, so be careful.

Access to the controller and dashboard is available via HTTPS over port 443, and
a randomly generated bearer token is used for authentication. The TLS
certificate used for communication is generated during installation.
A cryptographic hash of the certificate is pinned as part of the CLI
configuration string to prevent man-in-the-middle attacks.

## Dashboard CA Certificate

A CA certificate is also generated during installation and signs the certificate
presented by the controller and dashboard. The private key corresponding
associated with the CA certificate is discarded immediately to prevent misuse.
The CA certificate is required as a work-around for how browsers handle multiple
connections to servers with self-signed certificates. The CA certificate is
provided to the browser by the dashboard in order to allow TLS-encrypted
communication with the dashboard and the controller. If the Flynn installer is
used, this CA certificate is transferred over SSH during installation and will
prevent MITM attacks when visiting the dashboard. If the installer is not used,
the certificate may be provided over an insecure connection the first time the
dashboard is visited, do not install the certificate if the connection is not
trusted. In the future we will use [Let's Encrypt](https://letsencrypt.org) to
avoid the need for the generated certificates and trust bootstrapping.

## Applications

Applications run within Flynn are not fully sandboxed and have access to
internal Flynn APIs that can be used to gain root access on the server. Do not
run untrusted code in Flynn.

There may be other unknown security flaws in Flynn. For the time being we do not
recommend running Flynn in environments where there is access to sensitive data
or services.

We are currently working to implement and improve basic security practices in
Flynn so that it is secure by default, inside and outside. After that, the sky
is the limit. Expect to see industry-leading security policies and practices
baked into Flynn in the future.

## Reporting Issues

If you discover a security flaw in Flynn that is not explicitly acknowledged
here, please email us at [security@flynn.io](mailto:security@flynn.io)
immediately. We will acknowledge your email as soon as we receive it, within 24
hours, and provide a more detailed response within 48 hours clearly indicating
next steps. If you have the ability to use PGP, you can encrypt your email using
the public key below.

After the initial reply to your report, we will keep you informed of the
progress being made towards a fix and release. These updates will be sent
frequently, usually at least every 24-48 hours.

If you do not get a reply to your initial email within 48 hours, please contact
[a member of the Flynn team](https://github.com/orgs/flynn/people) directly and
ask them to put the security team in touch with you immediately.

If you believe an [existing issue](https://github.com/flynn/flynn/issues) is
security-related, please send an email to
[security@flynn.io](mailto:security@flynn.io) with the issue number and
a description of why it should be treated as a security issue.

## Disclosure Process

Flynn uses the following coordinated security disclosure process for security
issues:

1. Once a security report is received, it is assigned to a primary handler. This
   person coordinates the process of communicating with the reporter, fixing the
   bug, notifying users, and releasing an update.
1. The issue is confirmed and the affected components and versions are
   determined.
1. If a CVE-ID is necessary, one is obtained.
1. Fixes are prepared for the current and previous stable release and the master
   branch. These fixes are not committed to the public repository.
1. An advance notification is sent to the release announcements mailing list to
   allow users to prepare to apply the fix.
1. Three days after the notification, the fix is applied to the public
   repository and new stable and nightly releases are pushed. The stable release
   will be a hotfix release that only contains the security fix.
1. As soon as the fix is applied and released, announcements are sent to the
   release announcements mailing list and posted on the blog. The announcement
   will contain information on the affected versions, how to update, as well as
   available options for working around the issue.

The three-day advance announcement may be skipped at our discretion if
a security issue becomes known publicly, is determined to be already exploited
in the wild, or is in an upstream component that we depend on and we have no
advance notice of the issue.

_This policy is based on the [Go security disclosure policy](https://golang.org/security)._

## Security Announcements

The best way to receive security announcements is to subscribe to the release
announcements mailing list. Any messages related to a security issue will have
the word `security` in the subject.

<form action="https://flynn.us7.list-manage.com/subscribe/post?u=9600741fc187618e1baa39a58&id=8aadb709f3" method="post" target="_blank" novalidate class="mailing-list-form">
  <label>Email Address&nbsp;
    <input type="email" name="EMAIL" placeholder="you@example.com">
  </label>
  <button type="submit" name="subscribe">Subscribe</button>
</form>

## PGP Key

```text
pub   4096R/6913B2EF 2015-11-04 [expires: 2016-11-03]
      Key fingerprint = C334 DB91 6744 BD00 B347  0A86 0281 AD75 6913 B2EF
uid                  Flynn Security Team <security@flynn.io>
sub   4096R/38F74B09 2015-11-04 [expires: 2016-11-03]

-----BEGIN PGP PUBLIC KEY BLOCK-----

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
aOmKnAD2rS4Lu4+Ez2pTZFz9
=yreI
-----END PGP PUBLIC KEY BLOCK-----
```
