## Options to set on the command line
d-i debian-installer/locale string en_US.utf8
d-i console-setup/ask_detect boolean false
d-i console-setup/layout string USA

#d-i netcfg/get_hostname string dummy
d-i netcfg/get_hostname string unassigned-hostname
d-i netcfg/get_domain string unassigned-domain

# Continue without a default route
# Not working , specify a dummy in the DHCP
#d-i netcfg/no_default_route boolean

d-i time/zone string UTC
d-i clock-setup/utc-auto boolean true
d-i clock-setup/utc boolean true

d-i kbd-chooser/method select American English

d-i netcfg/wireless_wep string

d-i base-installer/kernel/override-image string linux-server
#d-i base-installer/kernel/override-image string linux-image-2.6.32-21-generic

# Choices: Dialog, Readline, Gnome, Kde, Editor, Noninteractive
d-i debconf debconf/frontend select Noninteractive

d-i pkgsel/install-language-support boolean false
tasksel tasksel/first multiselect standard, ubuntu-server

#d-i partman-auto/method string regular
d-i partman-auto/method string lvm
#d-i partman-auto/purge_lvm_from_device boolean true

d-i partman-lvm/confirm boolean true
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-auto/choose_recipe select atomic

d-i partman/confirm_write_new_label boolean true
d-i partman/confirm_nooverwrite boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true

#http://ubuntu-virginia.ubuntuforums.org/showthread.php?p=9626883
#Message: "write the changes to disk and configure lvm preseed"
#http://serverfault.com/questions/189328/ubuntu-kickstart-installation-using-lvm-waits-for-input
#preseed partman-lvm/confirm_nooverwrite boolean true

# Write the changes to disks and configure LVM?
d-i partman-lvm/confirm boolean true
d-i partman-lvm/confirm_nooverwrite boolean true
d-i partman-auto-lvm/guided_size string max

## Default user, we can get away with a recipe to change this
d-i passwd/user-fullname string vagrant
d-i passwd/username string vagrant
d-i passwd/user-password password vagrant
d-i passwd/user-password-again password vagrant
d-i user-setup/encrypt-home boolean false
d-i user-setup/allow-password-weak boolean true

## minimum is puppet and ssh and ntp
# Individual additional packages to install
d-i pkgsel/include string openssh-server ntp

# Whether to upgrade packages after debootstrap.
# Allowed values: none, safe-upgrade, full-upgrade
d-i pkgsel/upgrade select full-upgrade

d-i grub-installer/only_debian boolean true
d-i grub-installer/with_other_os boolean true
d-i finish-install/reboot_in_progress note

#For the update
d-i pkgsel/update-policy select none

# debconf-get-selections --install
#Use mirror
#d-i apt-setup/use_mirror boolean true
#d-i mirror/country string manual
#choose-mirror-bin mirror/protocol string http
#choose-mirror-bin mirror/http/hostname string 192.168.4.150
#choose-mirror-bin mirror/http/directory string /ubuntu
#choose-mirror-bin mirror/suite select maverick
#d-i debian-installer/allow_unauthenticated string true

choose-mirror-bin mirror/http/proxy string
