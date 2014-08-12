---
title: Flynn Demo and Roadmap
date: November 14, 2013
---

Greetings from Flynn!

Lots of news from the world of Flynn today including the first demo of Flynn,
a video of our first meetup, and a status report.

Today we are launching the first [Flynn
demo](https://github.com/flynn/flynn/tree/master/demo). The demo includes basic
features of Flynn layers 0 and 1 including the host service, service discovery,
and git receive. The demo is [on
GitHub](https://github.com/flynn/flynn/tree/master/demo) and a screencast is
embedded below. After you try the demo check out some of the underlying
components also on the [Flynn GitHub repo](https://github.com/flynn/flynn).

<video controls="true" poster="https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.jpeg" width="640" height="360">
  <source src="https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.mp4" type="video/mp4">
  <source src="https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.webm" type="video/webm">
  <img alt="Flynn Demo" src="https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.jpeg" width="640" height="360" title="No video playback capabilities, please download the video." />
</video>

Thanks to everyone who attended our first community meetup last week in San
Francisco. As promised, a video of the event was recorded, and is embedded
below. A special thank you to Twilio for hosting the event.

<video controls="true" preload="none" poster="https://s3.amazonaws.com/flynn-media/flynn_meetup_2013-11-05.jpeg" width="640" height="360">
  <source src="https://s3.amazonaws.com/flynn-media/flynn_meetup_2013-11-05_720p.mp4" type="video/mp4">
  <source src="https://s3.amazonaws.com/flynn-media/flynn_meetup_2013-11-05_720p.webm" type="video/webm">
  <img alt="Flynn Meetup" src="https://s3.amazonaws.com/flynn-media/flynn_meetup_2013-11-05.jpeg" width="640" height="360" title="No video playback capabilities, please download the video." />
</video>

### Where we are

Basic versions of most components already exist. There are only a few remaining
questions about the overall architecture, most of which will be resolved in the
near future.

### What's next

Next month we will announce a more polished, complete, and documented version of
the Layer 0 components: [the host
service](https://github.com/flynn/flynn/tree/master/host), [scheduling
framework](https://github.com/flynn/flynn/tree/master/host/sampi), and [service
discovery](https://github.com/flynn/flynn/tree/master/discoverd). This layer can
be used on its own to schedule and run jobs (e.g. replace Mesos). A similar
release of the major Layer 1 components will follow in early 2014. This will
likely include the [management
API](https://github.com/flynn/flynn/tree/master/controller), [git
receiver](https://github.com/flynn/flynn/tree/master/gitreceived), support for
[Heroku](https://github.com/flynn/flynn/tree/master/slugbuilder)
[buildpacks](https://github.com/flynn/flynn/tree/master/slugrunner), and the
[router](https://github.com/flynn/flynn/tree/master/router). Other Layer
1 components will arrive later in the year.

### Money

Our team has put more time into Flynn than expected (less than half the time
spent has been paid). We'll run out of funds entirely around the Layer 1 release
in early 2014. We really want to continue our work on Flynn, especially as
companies are starting to deploy Flynn — we want to be there to provide bug
fixes, security updates, and additional features throughout the year. We're
asking for an additional $350,000 for 2014 to support the existing team for the
year and possibly bring in a few additional developers. We also expect to see
significant contributions in the form of pull requests from users of Flynn as
the project evolves. If we reach this goal we expect Flynn to be extremely
stable and feature-rich by the end of 2014. Based on the results of our first
campaign, we are focusing on companies who can contribute on a monthly recurring
basis, but of course all types and amounts are appreciated.

We encourage everyone to check out the demo and dive into the code. We will be
in [IRC](irc://irc.freenode.net/flynn) and on
[GitHub](https://github.com/flynn/flynn) to answer any questions and respond to
comments. You can also email us anytime.

Thank you for your support and encouragement and being part of the Flynn
community.

—The Flynn Team (Jonathan, Jeff, and Daniel)
