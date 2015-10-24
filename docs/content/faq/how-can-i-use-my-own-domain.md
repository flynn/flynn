---
title: How can I use my own domain in Flynn?
layout: docs
toc_min_level: 2
---

# How can I use my own domain in Flynn?

Flynn clusters are provisioned with a flynnhub.com domain under which all hostnames — including apps — reside, e.g. wordpress-master-master.evoa.flynnhub.com. The simplest way to use your own domain is to add a CNAME for each app.

    wordpress.mydomain.com. IN CNAME wordpress-master-master.evoa.flynnhub.com

You must also add a route for your domain to the app, either in the dashboard or via the CLI.

    # Add a route using the `flynn route` command
    $ flynn route add http wordpress.mydomain.com

![adding a domain](/images/docs/add-domain.png)

If you want to bypass the use of flynnhub.com altogether, you will have to add an A record in your domain for every host in your Flynn cluster.

    flynn.mydomain.com. IN A 10.0.0.250
    flynn.mydomain.com. IN A 10.0.0.251
    flynn.mydomain.com. IN A 10.0.0.252

You can then point your app's CNAME at that hostname, e.g. flynn.mydomain.com. Remember that any time a host is added or removed from your Flynn cluster, you must update your DNS to reflect this.
