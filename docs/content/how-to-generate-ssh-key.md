---
title: How to generate an SSH key for Flynn
layout: docs
---

# How to generate an SSH key for Flynn

SSH keys are a more secure alternative to password authentication. A key is actually a pair of keys, a private key which is just that — private and never to leave your computer — and a matching public key to be uploaded to any servers you wish to which you want to login. Flynn uses the SSH key to securely transmit app source code.

1. **Run ssh-keygen to generate an SSH key.** RSA is the default key type on most systems.
    
    ```
    ssh-keygen -t rsa
    ```
2. **Save your key to the default location.** This will give you a pair of files in the `.ssh/` directory off of your home directory - For RSA keys, your private key will be `.ssh/id_rsa`, and your public key `.ssh/id_rsa.pub`.
    
    ```
    Generating public/private rsa key pair.
    Enter file in which to save the key (/Users/username/.ssh/id_rsa):
    ```
3. **Set a passphrase.** While a passphrase is optional, we recommend that you use a passphrase to further secure your SSH keys. Anyone with your unencrypted private key can login to servers with your public key on them. Securing your private key with a complex passphrase makes it practically impossible for malicious actors to use your key. For convenience, SSH includes an agent which can store an unencrypted copy of the key in memory while it's running, and on OS X it can be saved in your keychain.
    
    ```
    Enter passphrase (empty for no passphrase):
    Enter same passphrase again:
    ```
Once your key is generated, you can begin to deploy apps from the command line to Flynn. You can also upload your public key to remote servers by adding the contents of your `.ssh/id_rsa.pub` file to `~/.ssh/authorized_keys` on remote hosts.
