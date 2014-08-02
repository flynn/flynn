# flynn-devbox

flynn-devbox contains a Vagrantfile and scripts to make working on Flynn
components easy.

## Usage

Once you have the box running (`vagrant up`) and have connected to it (`vagrant
ssh`), there are three useful scripts available:

- `checkout-flynn` clones many of the Flynn repos into `src` (this is available
  from the host and the guest)
- `build-flynn` compiles all of the cloned repos, run this after modifying code
- `bootstrap-flynn` boots a Flynn cluster, run this after modifying code and
  running `build-flynn`
