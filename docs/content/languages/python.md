---
title: Python
layout: docs
---

# Python

Python is supported by the [Python
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

The Python buildpack supports both the latest Python 2 and Python 3 runtimes, as well as PyPy runtimes. See the list of supported runtimes at [the buildpack's GitHub page](https://github.com/heroku/heroku-buildpack-python/tree/master/builds/runtimes).

## Default Process Types

No default process types are defined for this buildpack, so a `Procfile` is needed. To deploy [Gunicorn](http://gunicorn.org), for example, your `Procfile` might look like this:

```
web: gunicorn hello:app --log-file -
```
