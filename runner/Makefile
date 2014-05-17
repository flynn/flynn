flynn-test.tar.xz: rootfs/rootfs.img uml/linux
	tar -Sc -C rootfs rootfs.img -C ../uml linux | xz -ve9 - > flynn-test.tar.xz

rootfs/rootfs.img:
	cd rootfs && make

uml/linux:
	cd uml && make

.PHONY: clean
clean:
	cd rootfs && make clean
	cd uml && make clean
	rm -f flynn-test.tar.xz
