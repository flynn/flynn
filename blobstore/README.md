# Blobstore

A simple, fast HTTP file service.

Blobstore provides a simple HTTP interface for reading, writing, and deleting
files. It is like a simpler S3. All operations fall under these three HTTP
verbs:

 * PUT: write a file: `curl -X PUT -T /path/to/local/file
   http://blobstore.discoverd/path/to/remote/file`
 * GET: read a file: `curl http://blobstore.discoverd/path/to/remote/file`
 * DELETE: delete a file: `curl -X DELETE
   http://blobstore.discoverd/path/to/remote/file`

Files can be copied by setting the `Blobstore-Copy-From` header in a PUT
request:

```shell
# create /file1.txt
echo data | curl -X PUT --data-binary @- http://blobstore.discoverd/file1.txt

# copy it to /file2.txt
curl -X PUT --header "Blobstore-Copy-From: /file1.txt" http://blobstore.discoverd/file2.txt

# read /file2.txt
curl http://blobstore.discoverd/file2.txt
data

```


Parent directories are automatically created, and directory indexes can be
accessed with a GET request to the root path with the `dir` query param:

```shell
# create 4 files in different directories
curl -X PUT --data-binary "data" http://blobstore.discoverd/foo.txt
curl -X PUT --data-binary "data" http://blobstore.discoverd/dir1/foo.txt
curl -X PUT --data-binary "data" http://blobstore.discoverd/dir2/foo.txt
curl -X PUT --data-binary "data" http://blobstore.discoverd/dir3/foo.txt

# list all top-level files and directories
curl http://blobstore.discoverd/
["/dir1/","/dir2/","/dir3/","/foo.txt"]

# list files in /dir1
curl http://blobstore.discoverd/?dir=/dir1
["/dir1/foo.txt"]

# list files in /dir2
curl http://blobstore.discoverd/?dir=/dir2
["/dir2/foo.txt"]
```

Right now, the files are stored as large objects in PostgreSQL or on the local
filesystem, but it's intended to provide a simple, pre-authenticated gateway to
S3 and maybe other file storage systems in the near future.

Flynn uses blobstore to store and retrieve Heroku-style slugs built with
[slugbuilder](/slugbuilder).
