flynn-test.tar.xz: rootfs/rootfs.img
	tar -Sc -C rootfs rootfs.img | xz -ve9 - > flynn-test.tar.xz

rootfs/rootfs.img:
	cd rootfs && make

.PHONY: clean
clean:
	cd rootfs && make clean
	rm -f flynn-test.tar.xz
