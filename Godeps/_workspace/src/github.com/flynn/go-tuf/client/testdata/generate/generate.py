#!/usr/bin/env python
#
# A script to generate TUF repository files.
#
# A modification of generate.py from the Python implementation:
# https://github.com/theupdateframework/tuf/blob/v0.9.9/tests/repository_data/generate.py

import shutil
import datetime
import optparse
import stat

from tuf.repository_tool import *
import tuf.util

parser = optparse.OptionParser()
parser.add_option("-c","--consistent-snapshot", action='store_true',  dest="consistent_snapshot",
    help="Generate consistent snapshot", default=False)
(options, args) = parser.parse_args()

repository = create_new_repository('repository')

root_key_file = 'keystore/root_key'
targets_key_file = 'keystore/targets_key'
snapshot_key_file = 'keystore/snapshot_key'
timestamp_key_file = 'keystore/timestamp_key'

generate_and_write_ed25519_keypair(root_key_file, password='password')
generate_and_write_ed25519_keypair(targets_key_file, password='password')
generate_and_write_ed25519_keypair(snapshot_key_file, password='password')
generate_and_write_ed25519_keypair(timestamp_key_file, password='password')

root_public = import_ed25519_publickey_from_file(root_key_file+'.pub')
targets_public = import_ed25519_publickey_from_file(targets_key_file+'.pub')
snapshot_public = import_ed25519_publickey_from_file(snapshot_key_file+'.pub')
timestamp_public = import_ed25519_publickey_from_file(timestamp_key_file+'.pub')

root_private = import_ed25519_privatekey_from_file(root_key_file, 'password')
targets_private = import_ed25519_privatekey_from_file(targets_key_file, 'password')
snapshot_private = import_ed25519_privatekey_from_file(snapshot_key_file, 'password')
timestamp_private = import_ed25519_privatekey_from_file(timestamp_key_file, 'password')

repository.root.add_verification_key(root_public)
repository.targets.add_verification_key(targets_public)
repository.snapshot.add_verification_key(snapshot_public)
repository.timestamp.add_verification_key(timestamp_public)

repository.root.load_signing_key(root_private)
repository.targets.load_signing_key(targets_private)
repository.snapshot.load_signing_key(snapshot_private)
repository.timestamp.load_signing_key(timestamp_private)

target1_filepath = 'repository/targets/file1.txt'
tuf.util.ensure_parent_dir(target1_filepath)
target2_filepath = 'repository/targets/dir/file2.txt'
tuf.util.ensure_parent_dir(target2_filepath)

with open(target1_filepath, 'wt') as file_object:
  file_object.write('file1.txt')

with open(target2_filepath, 'wt') as file_object:
  file_object.write('file2.txt')

octal_file_permissions = oct(os.stat(target1_filepath).st_mode)[4:]
file_permissions = {'file_permissions': octal_file_permissions}
repository.targets.add_target(target1_filepath, file_permissions)
repository.targets.add_target(target2_filepath)

repository.root.expiration = datetime.datetime(2030, 1, 1, 0, 0)
repository.targets.expiration = datetime.datetime(2030, 1, 1, 0, 0)
repository.snapshot.expiration = datetime.datetime(2030, 1, 1, 0, 0)
repository.timestamp.expiration = datetime.datetime(2030, 1, 1, 0, 0)

repository.targets.compressions = ['gz']

if options.consistent_snapshot:
  repository.write(False, True)

else:
  repository.write()

shutil.move('repository/metadata.staged', 'repository/metadata')
