# flynn-test

flynn-test contains full-stack acceptance tests for Flynn.

## Usage

### Bootstrap Flynn

The tests need a running Flynn cluster, so you will need to boot one first.

To run Flynn locally, first boot and SSH to the Flynn dev box:

```text
vagrant up
vagrant ssh
```

then build and bootstrap Flynn (this may take a few minutes):

```text
make
script/bootstrap-flynn
```

### Run the tests

Run the `flynn cluster add` command from the bootstrap output to add the cluster to your `~/.flynnrc` file, then run the tests:

```text
flynn cluster add ...
cd ~/go/src/github.com/flynn/flynn/test
bin/flynn-test --flynnrc ~/.flynnrc --cli `pwd`/../cli/cli
```

## Auto booting clusters

The test binary is capable of booting its own cluster to run the tests against, provided you are using a machine capable of running KVM.

### Build root filesystem + kernel

Before running the tests, you need a root filesystem and a Linux kernel capable of building and running Flynn.

To build these into `/tmp/flynn`:

```text
mkdir -p /tmp/flynn
sudo rootfs/build.sh /tmp/flynn
```

You should now have `/tmp/flynn/rootfs.img` and `/tmp/flynn/vmlinuz`.

### Build the tests

```text
go build -o flynn-test
```

### Download Flynn CLI

The tests interact with the VM cluster using the Flynn CLI, so you will need it locally.

Download it into the current directory:

```text
curl -sL -A "`uname -sp`" https://cli.flynn.io/flynn.gz | zcat >flynn
chmod +x flynn
```

### Run the tests

```text
sudo ./flynn-test \
  --user `whoami` \
  --rootfs /tmp/flynn/rootfs.img \
  --kernel /tmp/flynn/vmlinuz \
  --cli `pwd`/flynn
```

## CI

### Dependencies

```text
apt-add-repository 'deb http://ppa.launchpad.net/anatol/tup/ubuntu precise main'
apt-key adv --keyserver keyserver.ubuntu.com --recv E601AAF9486D3664
apt-get update
apt-get install -y zerofree qemu qemu-kvm tup
```

### Updating the runner

With the updated runner code checked out in your CI repo, build the runner:

```text
cd /path/to/flynn/test
go build -o bin/flynn-test-runner ./runner
```

If you need to rebuild the root filesystem, first stop the runner and unmount the build directory:

```
sudo stop flynn-test
sudo umount /opt/flynn-test/build
```

Run the update script:

```
sudo scripts/update-runner.sh
```

Since `/etc/default/flynn-test` may contain secrets, the above does not change it so you may have to make some manual edits if `scripts/defaults.conf` has changed.
