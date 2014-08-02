## Usage

Once you have the box running (`vagrant up`) and have connected to it (`vagrant
ssh`), these scripts are available:

- `build-flynn` compiles all of the cloned repos, run this after modifying code
- `bootstrap-flynn` boots a Flynn cluster, run this after modifying code and
  running `build-flynn`
