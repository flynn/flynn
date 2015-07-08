#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <sched.h>
#include <fcntl.h>
#include <sys/mount.h>

#define errExit(msg)  do { perror(msg); exit(EXIT_FAILURE); } while (0)

int main(int argc, char *argv[]) {
  if (argc < 3) {
    fprintf(stderr, "%s pid mount ...\n", argv[0]);
    exit(EXIT_FAILURE);
  }

  int pid = atoi(argv[1]);
  char file[64];
  sprintf(file, "/proc/%d/ns/mnt", pid);
  int fd = open(file, O_RDONLY);
  if (fd == -1) {
    errExit("open");
  }

  if (setns(fd, 0) == -1) {
    errExit("setns");
  }

  int error = 0;
  for (int i = 2; i < argc; i++) {
    if (umount(argv[i]) != 0) {
      error = 1;
      perror(argv[i]);
    }
  }

  if (error) {
    exit(EXIT_FAILURE);
  }
}
