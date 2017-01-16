Run VMs in Flynn using KVM
==========================

See `./run.sh --help` for usage.

Ubuntu Cloud Image
------------------

* build the Flynn KVM image:

```
sudo ../../util/imagebuilder/build-image kvm > kvm.json
```

* install tools

```
sudo apt-get install --yes qemu-utils cloud-utils
```

* download image

```
curl -o ubuntu-xenial.img https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img
```

* convert to qcow2 (keep original clean)

```
qemu-img convert -O qcow2 ubuntu-xenial.img disk.img
```

* create user-data

```
cat > user-data <<EOF
#cloud-config
password: ubuntu
chpasswd: { expire: False }
ssh_pwauth: True
EOF

cloud-localds user-data.img user-data
```

* run VM

```
./run.sh --kvm-args '-m 2048 -smp 4' disk.img user-data.img
```
