#!/usr/bin/env python
#
# A script to download updates from a remote TUF repository.
#
# A modification of basic_client.py from the Python implementation:
# https://github.com/theupdateframework/tuf/blob/v0.9.9/tuf/client/basic_client.py

"""
<Program Name>
  basic_client.py

<Author>
  Vladimir Diaz <vladimir.v.diaz@gmail.com>

<Started>
  September 2012

<Copyright>
  See LICENSE for licensing information.

<Purpose>
  Provide a basic TUF client that can update all of the metatada and target
  files provided by the user-specified repository mirror.  Updated files are
  saved to the 'targets' directory in the current working directory.  The
  repository mirror is specified by the user through the '--repo' command-
  line option.

  Normally, a software updater integrating TUF will develop their own costum
  client module by importing 'tuf.client.updater', instantiating the required
  object, and calling the desired methods to perform an update.  This basic
  client is provided to users who wish to give TUF a quick test run without
  the hassle of writing client code.  This module can also used by updaters that
  do not need the customization and only require their clients to perform an
  update of all the files provided by their repository mirror(s).

  For software updaters that DO require customization, see the 'example_client.py'
  script.  The 'example_client.py' script provides an outline of the client code
  that software updaters may develop and then tailor to their specific software
  updater or package manager.

  Additional tools for clients running legacy applications will also be made
  available.  These tools will allow secure software updates using The Update
  Framework without the need to modify the original application.

<Usage>
  $ python basic_client.py --repo http://localhost:8001
  $ python basic_client.py --repo http://localhost:8001 --verbose 3

<Options>
  --verbose:
    Set the verbosity level of logging messages.  Accepts values 1-5.

  --repo:
    Set the repository mirror that will be responding to client requests.
    E.g., 'http://locahost:8001'.
"""

# Help with Python 3 compatibility, where the print statement is a function, an
# implicit relative import is invalid, and the '/' operator performs true
# division.  Example:  print 'hello world' raises a 'SyntaxError' exception.
from __future__ import print_function
from __future__ import absolute_import
from __future__ import division
from __future__ import unicode_literals

import sys
import traceback
import optparse
import logging

import tuf
import tuf.formats
import tuf.client.updater
import tuf.log

# See 'log.py' to learn how logging is handled in TUF.
logger = logging.getLogger('tuf.basic_client')


def update_client(repository_mirror):
  """
  <Purpose>
    Perform an update of the metadata and target files located at
    'repository_mirror'.  Target files are saved to the 'targets' directory
    in the current working directory.  The current directory must already
    include a 'metadata' directory, which in turn must contain the 'current'
    and 'previous' directories.  At a minimum, these two directories require
    the 'root.json' metadata file.

  <Arguments>
    repository_mirror:
      The URL to the repository mirror hosting the metadata and target
      files.  E.g., 'http://localhost:8001'

  <Exceptions>
    tuf.RepositoryError, if 'repository_mirror' is improperly formatted.

  <Side Effects>
    Connects to a repository mirror and updates the metadata files and
    any target files.  Obsolete targets are also removed locally.

  <Returns>
    None.
  """

  # Does 'repository_mirror' have the correct format?
  try:
    tuf.formats.URL_SCHEMA.check_match(repository_mirror)
  except tuf.FormatError as e:
    message = 'The repository mirror supplied is invalid.' 
    raise tuf.RepositoryError(message)
  
  # Set the local repository directory containing all of the metadata files.
  tuf.conf.repository_directory = '.'

  # Set the repository mirrors.  This dictionary is needed by the Updater
  # class of updater.py.
  repository_mirrors = {'mirror': {'url_prefix': repository_mirror,
                                  'metadata_path': 'repository',
                                  'targets_path': 'repository/targets',
                                  'confined_target_dirs': ['']}}

  # Create the repository object using the repository name 'repository'
  # and the repository mirrors defined above.
  updater = tuf.client.updater.Updater('repository', repository_mirrors)

  # The local destination directory to save the target files.
  destination_directory = './targets'

  # Refresh the repository's top-level roles, store the target information for
  # all the targets tracked, and determine which of these targets have been
  # updated.
  updater.refresh()
  all_targets = updater.all_targets()
  updated_targets = updater.updated_targets(all_targets, destination_directory)

  # Download each of these updated targets and save them locally.
  for target in updated_targets:
    try: 
      updater.download_target(target, destination_directory)
    except tuf.DownloadError as e:
      pass

  # Remove any files from the destination directory that are no longer being
  # tracked.
  updater.remove_obsolete_targets(destination_directory)





def parse_options():
  """
  <Purpose>
    Parse the command-line options and set the logging level
    as specified by the user through the --verbose option.
    'basic_client' expects the '--repo' to be set by the user.

    Example:
      $ python basic_client.py --repo http://localhost:8001

    If the required option is unset, a parser error is printed
    and the scripts exits.

  <Arguments>
    None.

  <Exceptions>
    None.

  <Side Effects>
    Sets the logging level for TUF logging.

  <Returns>
    The 'options.REPOSITORY_MIRROR' string.
  """

  parser = optparse.OptionParser()

  # Add the options supported by 'basic_client' to the option parser.
  parser.add_option('--verbose', dest='VERBOSE', type=int, default=2,
                    help='Set the verbosity level of logging messages.'
                         'The lower the setting, the greater the verbosity.')

  parser.add_option('--repo', dest='REPOSITORY_MIRROR', type='string',
                    help='Specifiy the repository mirror\'s URL prefix '
                    '(e.g., http://www.example.com:8001/tuf/).'
                    ' The client will download updates from this mirror.')

  options, args = parser.parse_args()

  # Set the logging level.
  if options.VERBOSE == 5:
    tuf.log.set_log_level(logging.CRITICAL)
  elif options.VERBOSE == 4:
    tuf.log.set_log_level(logging.ERROR)
  elif options.VERBOSE == 3:
    tuf.log.set_log_level(logging.WARNING)
  elif options.VERBOSE == 2:
    tuf.log.set_log_level(logging.INFO)
  elif options.VERBOSE == 1:
    tuf.log.set_log_level(logging.DEBUG)
  else:
    tuf.log.set_log_level(logging.NOTSET)

  # Ensure the '--repo' option was set by the user.
  if options.REPOSITORY_MIRROR is None:
    message = '"--repo" must be set on the command-line.'
    parser.error(message)
    
  # Return the repository mirror containing the metadata and target files.
  return options.REPOSITORY_MIRROR



if __name__ == '__main__':
  
  # Parse the options and set the logging level.
  repository_mirror = parse_options()

  # Perform an update of all the files in the 'targets' directory located in
  # the current directory.
  try:
    update_client(repository_mirror)
  
  except (tuf.NoWorkingMirrorError, tuf.RepositoryError) as e:
    traceback.print_exc()
    sys.stderr.write('Error: '+str(e)+'\n')
    sys.exit(1)

  # Successfully updated the client's target files.
  sys.exit(0)
