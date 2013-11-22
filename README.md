# Shelf
An HTTP File Service

Shelf provides a simple HTTP interface for reading, writing, and deleting files. It's like a simpler S3. All operations fall under these three HTTP verbs:

 * PUT: write a file:
	curl -X PUT -T /path/to/local/file http://shelfhost/path/to/remote/file
 * GET: read a file:
 	curl http://shelfhost/path/to/remote/file
 * DELETE: delete a file:
 	curl -X DELETE http://shelfhost/path/to/remote/file

There are no directory indexes. Parent directories are automatically created. Right now, the files are stored on the filesystem (wherever you point it), but it's intended to provide a simple, pre-authenticated endpoint for S3 and maybe other file storage systems in the near future.

## License

BSD