VERSION := 3.14.4
AUFS_VERSION := 3.14

linux.xz: linux-$(VERSION)/linux
	xz --stdout linux-$(VERSION)/linux > linux.xz

linux-$(VERSION)/linux: linux-$(VERSION)/.aufs-patched linux-$(VERSION)/.config
	cd linux-$(VERSION) && ARCH=um make -j8 && strip linux

linux-$(VERSION)/.aufs-patched: linux-$(VERSION)/Makefile aufs3-standalone/README
	rm -f aufs3-standalone/include/uapi/linux/Kbuild
	cd aufs3-standalone && cp -r Documentation fs include ../linux-$(VERSION)
	cd linux-$(VERSION) && cat ../aufs3-standalone/*.patch | patch -N -p1
	touch linux-$(VERSION)/.aufs-patched

aufs3-standalone/README:
	git clone http://git.code.sf.net/p/aufs/aufs3-standalone
	cd aufs3-standalone && git checkout aufs$(AUFS_VERSION)
	touch aufs3-standalone/README

linux-$(VERSION)/.config: linux-$(VERSION)/Makefile
	cp linux-config linux-$(VERSION)/.config
	touch linux-$(VERSION)/.config

linux-$(VERSION)/Makefile:
	curl -L https://www.kernel.org/pub/linux/kernel/v3.x/linux-$(VERSION).tar.xz | xz -d | tar x
	touch linux-$(VERSION)/Makefile

.PHONY: clean
clean:
	rm -rf linux-$(VERSION) aufs3-standalone
