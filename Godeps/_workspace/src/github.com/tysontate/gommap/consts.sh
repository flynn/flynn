#!/bin/bash
gcc -xc -Wall -pedantic consts.c.txt -o _consts.out
./_consts.out > consts.go
rm ./_consts.out
