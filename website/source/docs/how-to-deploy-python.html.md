---
title: How To Deploy Python
layout: docs
---

# How To Deploy Python

Python is supported by the [Heroku Python
buildpack](https://github.com/heroku/heroku-buildpack-python).

## Detection

The Python buildpack is used if the repository contains a `requirements.txt`
file. Django applications are detected by the presence of a `manage.py` file.
If the `manage.py` file is found, `manage.py collectstatic` is run during the
compilation process.

## Dependencies

Dependencies are managed using [`pip`](https://pypi.python.org/pypi/pip). Dependencies are specified in a `requirements.txt` file. For example:

```
Flask==0.9
```

## Specifying a Runtime

Deploys default to a recent version of Python 2.7. A different runtime version can be specified by providing a `runtime.txt` file, such as:

```
python-2.7.8
```

### Supported Runtimes

The latest `python-2.7` and `python-3.4` are officially supported, but any
runtime between 2.4.4–3.4.1 can be used, including PyPy runtimes. See
[the buildpack's GitHub page](https://github.com/heroku/heroku-buildpack-python/tree/master/builds/runtimes)
for a full list.

## Default Process Types

No default process types are defined for this buildpack, so a `Procfile` is needed. To deploy [Gunicorn](http://gunicorn.org), for example, your `Procfile` might look like this:

```
web: gunicorn hello:app --log-file -
```
