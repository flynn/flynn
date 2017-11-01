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
communication with the dashboard and the controller. The certificate may be
provided over an insecure connection the first time the dashboard is visited, do
not install the certificate if the connection is not trusted. In the future we
will use [Let's Encrypt](https://letsencrypt.org) to avoid the need for the
generated certificates and trust bootstrapping.

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
pub   4096R/6913B2EF 2015-11-04 [expires: 2018-11-01]
      Key fingerprint = C334 DB91 6744 BD00 B347  0A86 0281 AD75 6913 B2EF
uid                  Flynn Security Team <security@flynn.io>

-----BEGIN PGP PUBLIC KEY BLOCK-----

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
bHZ4SwrYlZ8uJvof0qXzzA==
=WncR
-----END PGP PUBLIC KEY BLOCK-----
```
