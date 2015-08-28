#!/bin/bash

if [[ -z "${HOME}" ]] || [[ "${HOME}" == "/" ]] ; then
  export HOME=/root
fi

# If there is a SSH private key available in the environment, save it so that it can be used
if [[ -n "${SSH_CLIENT_KEY}" ]]; then
  mkdir -p ${HOME}/.ssh
  file="${HOME}/.ssh/id_rsa"
  echo "${SSH_CLIENT_KEY}" > ${file}
  chmod 600 ${file}
fi

# If there is a list of known SSH hosts available in the environment, save it so that it can be used
if [[ -n "${SSH_CLIENT_HOSTS}" ]]; then
  mkdir -p ${HOME}/.ssh
  file="${HOME}/.ssh/known_hosts"
  echo "${SSH_CLIENT_HOSTS}" > ${file}
  chmod 600 ${file}
fi

exec /bin/gitreceived
