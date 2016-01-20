package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

var srcPlatform = Platform{"", ""}

var defaultPlatforms = []Platform{
	Platform{"linux", "386"},
	Platform{"linux", "amd64"},
	Platform{"freebsd", "amd64"},
	Platform{"darwin", "amd64"},
	Platform{"windows", "386"},
	Platform{"windows", "amd64"},
}

const (
	oldDistURL           = "https://go.googlecode.com/files/go%s.%s.tar.gz"
	distURL              = "https://storage.googleapis.com/golang/go%s.%s.tar.gz"
	lastOldDistVersion   = "1.2.1"
	lastOldDarwinVersion = "1.4.2"
)

type Platform struct {
	OS   string
	Arch string
}

func (p *Platform) String() string {
	if p.OS == "" && p.Arch == "" {
		return "src"
	}
	return p.OS + "_" + p.Arch
}

func (p *Platform) Download(version string) (path string, err error) {
	url := p.distURL(version)
	lg := Log.New("plat", p.String(), "url", url)
	lg.Info("start download")
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Bad response for download (%s): %v", url, resp.StatusCode)
	}

	archive, err := download(lg, resp.Body, p.String(), checksums[url])
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, os.SEEK_SET); err != nil {
		return "", err
	}

	path, err = ioutil.TempDir(".", p.String()+"-")
	if err != nil {
		return
	}
	var unpackFn func(string, *os.File) error
	switch {
	case strings.HasSuffix(url, ".zip"):
		unpackFn = unpackZip
	case strings.HasSuffix(url, ".tar.gz"):
		unpackFn = unpackTarGz
	default:
		return "", fmt.Errorf("Unknown archive type for URL: %v", url)
	}

	if err := unpackFn(path, archive); err != nil {
		lg.Error("unpack error", "err", err)
		return "", err
	}

	lg.Info("download complete")
	return path, nil
}

func (p *Platform) distURL(version string) string {
	template := distURL
	if version <= lastOldDistVersion {
		template = oldDistURL
	}

	distString := p.OS + "-" + p.Arch
	// special cases
	switch {
	case p.OS == "darwin" && version <= lastOldDarwinVersion:
		distString += "-osx10.8"
	case p.OS == "" && p.Arch == "":
		distString = "src"
	}

	s := fmt.Sprintf(template, version, distString)
	if p.OS == "windows" {
		s = strings.Replace(s, ".tar.gz", ".zip", 1)
	}
	return s
}

func download(lg log15.Logger, rd io.Reader, name string, checksum string) (*os.File, error) {
	f, err := ioutil.TempFile(".", name+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	sha := sha1.New()
	wr := io.MultiWriter(f, sha)
	if _, err := io.Copy(wr, rd); err != nil {
		return nil, err
	}
	if checksum == "" {
		lg.Warn("no checksum for URL")
	} else if actual := hex.EncodeToString(sha.Sum(nil)); actual != checksum {
		lg.Error("checksum mismatch", "expected", checksum, "got", actual)
		return nil, fmt.Errorf("checksum mismatch: %v/%v", actual, checksum)
	}
	return f, nil
}

var checksums = map[string]string{
	"https://storage.googleapis.com/golang/go1.5.2.src.tar.gz":                     "c7d78ba4df574b5f9a9bb5d17505f40c4d89b81c",
	"https://storage.googleapis.com/golang/go1.5.2.darwin-amd64.tar.gz":            "4f30332a56e9c8a36daeeff667bab3608e4dffd2",
	"https://storage.googleapis.com/golang/go1.5.2.darwin-amd64.pkg":               "102b4e946b7bb40f0e8aa508e41340a696ead752",
	"https://storage.googleapis.com/golang/go1.5.2.freebsd-amd64.tar.gz":           "34bbe347a95908ca440e4bf584a200522bba1985",
	"https://storage.googleapis.com/golang/go1.5.2.linux-386.tar.gz":               "49ff1c2510eaba80423e55a633901464b28437ef",
	"https://storage.googleapis.com/golang/go1.5.2.linux-amd64.tar.gz":             "cae87ed095e8d94a81871281d35da7829bd1234e",
	"https://storage.googleapis.com/golang/go1.5.2.windows-386.zip":                "a9b265268a4632ad6f7ca8769e6a34eb1522f784",
	"https://storage.googleapis.com/golang/go1.5.2.windows-386.msi":                "31bf4feb763385cc6e87a4c2aac9d8c711e3b378",
	"https://storage.googleapis.com/golang/go1.5.2.windows-amd64.zip":              "5eb85b0eec36cfef05700935f2420b6104986733",
	"https://storage.googleapis.com/golang/go1.5.2.windows-amd64.msi":              "101a612d3ce65a51459667340d1991d594f9b8e7",
	"https://storage.googleapis.com/golang/go1.5.1.src.tar.gz":                     "0df564746d105f4180c2b576a1553ebca9d9a124",
	"https://storage.googleapis.com/golang/go1.5.1.darwin-amd64.tar.gz":            "02451b1f3b2c715edc5587174e35438982663672",
	"https://storage.googleapis.com/golang/go1.5.1.darwin-amd64.pkg":               "857b77a85ba111af1b0928a73cca52136780a75d",
	"https://storage.googleapis.com/golang/go1.5.1.freebsd-amd64.tar.gz":           "78ac27b7c009142ed0d86b899f1711bb9811b7e1",
	"https://storage.googleapis.com/golang/go1.5.1.linux-386.tar.gz":               "6ce7328f84a863f341876658538dfdf10aff86ee",
	"https://storage.googleapis.com/golang/go1.5.1.linux-amd64.tar.gz":             "46eecd290d8803887dec718c691cc243f2175fe0",
	"https://storage.googleapis.com/golang/go1.5.1.windows-386.zip":                "bb071ec45ef39cd5ed9449b54c5dd083b8233bfa",
	"https://storage.googleapis.com/golang/go1.5.1.windows-386.msi":                "034065452b7233b2a570d4be1218a97c475cded0",
	"https://storage.googleapis.com/golang/go1.5.1.windows-amd64.zip":              "7815772347ad3e11a096d927c65bfb15d5b0f490",
	"https://storage.googleapis.com/golang/go1.5.1.windows-amd64.msi":              "0a439f49b546b82f85adf84a79bbf40de2b3d5ba",
	"https://storage.googleapis.com/golang/go1.4.3.src.tar.gz":                     "486db10dc571a55c8d795365070f66d343458c48",
	"https://storage.googleapis.com/golang/go1.4.3.darwin-amd64.tar.gz":            "945666c36b42bf859d98775c4f02f807a5bdb6b0",
	"https://storage.googleapis.com/golang/go1.4.3.darwin-amd64.pkg":               "3d91a21e3217370b80ca26e89a994e8199d583e7",
	"https://storage.googleapis.com/golang/go1.4.3.freebsd-amd64.tar.gz":           "573217c097f78143ea7c54212445c31944750144",
	"https://storage.googleapis.com/golang/go1.4.3.linux-386.tar.gz":               "405777725abe566989cdb436d2efeb2667be670f",
	"https://storage.googleapis.com/golang/go1.4.3.linux-amd64.tar.gz":             "332b64236d30a8805fc8dd8b3a269915b4c507fe",
	"https://storage.googleapis.com/golang/go1.4.3.windows-386.zip":                "77ec9b61c1e1bf475463c62c36c395ba9d69aa9e",
	"https://storage.googleapis.com/golang/go1.4.3.windows-386.msi":                "cad793895b258929ee796ef9ea77855626740ecd",
	"https://storage.googleapis.com/golang/go1.4.3.windows-amd64.zip":              "821a6773adadd7409380addc4791771f2b057fa0",
	"https://storage.googleapis.com/golang/go1.4.3.windows-amd64.msi":              "5e7c6cb012cbf09242b040b84b78b5e52d980337",
	"https://storage.googleapis.com/golang/go1.4.2.src.tar.gz":                     "460caac03379f746c473814a65223397e9c9a2f6",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-386-osx10.6.tar.gz":      "fb3e6b30f4e1b1be47bbb98d79dd53da8dec24ec",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-386-osx10.8.tar.gz":      "65f5610fdb38febd869aeffbd426c83b650bb408",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-386-osx10.6.pkg":         "3ed569ce33616d5d36f963e5d7cefb55727c8621",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-386-osx10.8.pkg":         "7f3fb2438fa0212febef13749d8d144934bb1c80",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-amd64-osx10.6.tar.gz":    "00c3f9a03daff818b2132ac31d57f054925c60e7",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-amd64-osx10.8.tar.gz":    "58a04b3eb9853c75319d9076df6f3ac8b7430f7f",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-amd64-osx10.6.pkg":       "3fa5455e211a70c0a920abd53cb3093269c5149c",
	"https://storage.googleapis.com/golang/go1.4.2.darwin-amd64-osx10.8.pkg":       "8fde619d48864cb1c77ddc2a1aec0b7b20406b38",
	"https://storage.googleapis.com/golang/go1.4.2.linux-386.tar.gz":               "50557248e89b6e38d395fda93b2f96b2b860a26a",
	"https://storage.googleapis.com/golang/go1.4.2.linux-amd64.tar.gz":             "5020af94b52b65cc9b6f11d50a67e4bae07b0aff",
	"https://storage.googleapis.com/golang/go1.4.2.windows-386.zip":                "0e074e66a7816561d7947ff5c3514be96f347dc4",
	"https://storage.googleapis.com/golang/go1.4.2.windows-386.msi":                "e8bd3d87cb52441b2c9aee7c2c5f5ce7ffccc832",
	"https://storage.googleapis.com/golang/go1.4.2.windows-amd64.zip":              "91b229a3ff0a1ce6e791c832b0b4670bfc5457b5",
	"https://storage.googleapis.com/golang/go1.4.2.windows-amd64.msi":              "a914f3dad5521a8f658dce3e1575f3b6792975f0",
	"https://storage.googleapis.com/golang/go1.4.src.tar.gz":                       "6a7d9bd90550ae1e164d7803b3e945dc8309252b",
	"https://storage.googleapis.com/golang/go1.4.darwin-386-osx10.6.tar.gz":        "ee31cd0e26245d0e48f11667e4298e2e7f54f9b6",
	"https://storage.googleapis.com/golang/go1.4.darwin-386-osx10.8.tar.gz":        "4d2ae2f5c0216c44e432c6044b1e1f0aea99f712",
	"https://storage.googleapis.com/golang/go1.4.darwin-386-osx10.6.pkg":           "05f2a1ab9d2aaae06c968fbdf1a6a9c28d380ceb",
	"https://storage.googleapis.com/golang/go1.4.darwin-386-osx10.8.pkg":           "81534c4eec80729b81b8e5f5889dfc2a3ba37131",
	"https://storage.googleapis.com/golang/go1.4.darwin-amd64-osx10.6.tar.gz":      "09621b9226abe12c2179778b015a33c1787b29d6",
	"https://storage.googleapis.com/golang/go1.4.darwin-amd64-osx10.8.tar.gz":      "28b2b731f86ada85246969e8ffc77d50542cdcb5",
	"https://storage.googleapis.com/golang/go1.4.darwin-amd64-osx10.6.pkg":         "29271b54d3ce7108270a9b7b64342950026704bf",
	"https://storage.googleapis.com/golang/go1.4.darwin-amd64-osx10.8.pkg":         "2043aaf5c1363e483c6042f8685acd70ec9e41f8",
	"https://storage.googleapis.com/golang/go1.4.freebsd-386.tar.gz":               "36c5cc2ebef4b4404b12f2b5f2dfd23d73ecdbcc",
	"https://storage.googleapis.com/golang/go1.4.freebsd-amd64.tar.gz":             "9441745b9c61002feedee8f0016c082b56319e44",
	"https://storage.googleapis.com/golang/go1.4.linux-386.tar.gz":                 "cb18d8122bfd3bbba20fa1a19b8f7566dcff795d",
	"https://storage.googleapis.com/golang/go1.4.linux-amd64.tar.gz":               "cd82abcb0734f82f7cf2d576c9528cebdafac4c6",
	"https://storage.googleapis.com/golang/go1.4.windows-386.zip":                  "f44240a1750dd051476ae78e9ad0502bc5c7661d",
	"https://storage.googleapis.com/golang/go1.4.windows-386.msi":                  "b26151702cba760d6eec94214c457bee01f6d859",
	"https://storage.googleapis.com/golang/go1.4.windows-amd64.zip":                "44f103d558b293919eb680041625c262dd00eb9a",
	"https://storage.googleapis.com/golang/go1.4.windows-amd64.msi":                "359124f2bba4c59df1eb81d11e16e388d0a996f9",
	"https://storage.googleapis.com/golang/go1.3.3.src.tar.gz":                     "b54b7deb7b7afe9f5d9a3f5dd830c7dede35393a",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-386-osx10.6.tar.gz":      "04b3e38549183e984f509c07ad40d8bcd577a702",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-386-osx10.8.tar.gz":      "88f35d3327a84107aac4f2f24cb0883e5fdbe0e5",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-386-osx10.6.pkg":         "49756b700670ae4109e555f2e5f9bedbaa3c50da",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-386-osx10.8.pkg":         "a89b570a326e5f8c9509f40be9fa90e54b3bf7a7",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-amd64-osx10.6.tar.gz":    "dfe68de684f6e8d9c371d01e6d6a522efe3b8942",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-amd64-osx10.8.tar.gz":    "be686ec7ba68d588735cc2094ccab8bdd651de9e",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-amd64-osx10.6.pkg":       "9aec7e9eff11100a6db026d1b423d1250925e4c4",
	"https://storage.googleapis.com/golang/go1.3.3.darwin-amd64-osx10.8.pkg":       "6435e50059fe7fa0d60f1b15aab7f255a61816ce",
	"https://storage.googleapis.com/golang/go1.3.3.freebsd-386.tar.gz":             "875a5515dd7d3e5826c7c003bb2450f3129ccbad",
	"https://storage.googleapis.com/golang/go1.3.3.freebsd-amd64.tar.gz":           "8531ae5e745c887f8dad1a3f00ca873cfcace56e",
	"https://storage.googleapis.com/golang/go1.3.3.linux-386.tar.gz":               "9eb426d5505de55729e2656c03d85722795dd85e",
	"https://storage.googleapis.com/golang/go1.3.3.linux-amd64.tar.gz":             "14068fbe349db34b838853a7878621bbd2b24646",
	"https://storage.googleapis.com/golang/go1.3.3.windows-386.zip":                "ba99083b22e0b22b560bb2d28b9b99b405d01b6b",
	"https://storage.googleapis.com/golang/go1.3.3.windows-386.msi":                "6017a0e1667a5a41109f527b405bf6e0c83580f5",
	"https://storage.googleapis.com/golang/go1.3.3.windows-amd64.zip":              "5f0b3b104d3db09edd32ef1d086ba20bafe01ada",
	"https://storage.googleapis.com/golang/go1.3.3.windows-amd64.msi":              "25112a8c4df93dc4009e65eff00bc4ef76f94e46",
	"https://storage.googleapis.com/golang/go1.3.2.src.tar.gz":                     "67d3a692588c259f9fe9dca5b80109e5b99271df",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-386-osx10.6.tar.gz":      "d1652f6e0ed3063b7b43d2bc12981d927bc85deb",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-386-osx10.8.tar.gz":      "d040c85698c749fdbe25e8568c4d71648a5e3a75",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-386-osx10.6.pkg":         "d20375615cf8e36e3c9a9b6ddeef16eff7a4ea89",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-386-osx10.8.pkg":         "f11930cfb032d39ab445f342742865c93c60ec14",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-amd64-osx10.6.tar.gz":    "36ca7e8ac9af12e70b1e01182c7ffc732ff3b876",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-amd64-osx10.8.tar.gz":    "323bf8088614d58fee2b4d2cb07d837063d7d77e",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-amd64-osx10.6.pkg":       "e1529241fcef643e5f752c37dc4c86911df91338",
	"https://storage.googleapis.com/golang/go1.3.2.darwin-amd64-osx10.8.pkg":       "fd8637658fcb133423e794c44029ce3476b48e0c",
	"https://storage.googleapis.com/golang/go1.3.2.freebsd-386.tar.gz":             "fea3ef264120b5c3b4c50a8929d56f47a8366503",
	"https://storage.googleapis.com/golang/go1.3.2.freebsd-amd64.tar.gz":           "95b633f45156fbbe79076638f854e76b9cd01301",
	"https://storage.googleapis.com/golang/go1.3.2.linux-386.tar.gz":               "3cbfd62d401a6ca70779856fa8ad8c4d6c35c8cc",
	"https://storage.googleapis.com/golang/go1.3.2.linux-amd64.tar.gz":             "0e4b6120eee6d45e2e4374dac4fe7607df4cbe42",
	"https://storage.googleapis.com/golang/go1.3.2.windows-386.zip":                "86160c478436253f51241ac1905577d337577ce0",
	"https://storage.googleapis.com/golang/go1.3.2.windows-386.msi":                "589c35f9ad3506c92aa944130f6a950ce9ee558b",
	"https://storage.googleapis.com/golang/go1.3.2.windows-amd64.zip":              "7f7147484b1bc9e52cf034de816146977d0137f6",
	"https://storage.googleapis.com/golang/go1.3.2.windows-amd64.msi":              "a697fff05cbd4a4d902f6c33f7c42588bcc474bc",
	"https://storage.googleapis.com/golang/go1.3.1.src.tar.gz":                     "bc296c9c305bacfbd7bff9e1b54f6f66ae421e6e",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-386-osx10.6.tar.gz":      "84f70a4c83be24cea696654a5b55331ea32f8a3f",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-386-osx10.8.tar.gz":      "244dfba1f4239b8e2eb9c3abae5ad63fc32c807a",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-386-osx10.6.pkg":         "16e0df7b90d49c8499f71a551af8b595e2faa961",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-386-osx10.8.pkg":         "13296cd9a980819bf2304d7d24a38a1b39719c13",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-amd64-osx10.6.tar.gz":    "40716361d352c4b40252e79048e8bc084c3f3d1b",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-amd64-osx10.8.tar.gz":    "a7271cbdc25173d0f8da66549258ff65cca4bf06",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-amd64-osx10.6.pkg":       "49bf5f14d2683fb99161fcb7025af60ec2d3691f",
	"https://storage.googleapis.com/golang/go1.3.1.darwin-amd64-osx10.8.pkg":       "5d4728e0b3c3fd9fc657cc192c6b9fb3f837823b",
	"https://storage.googleapis.com/golang/go1.3.1.freebsd-386.tar.gz":             "586debe95542b3b56841f6bd2e5257e301a1ffdc",
	"https://storage.googleapis.com/golang/go1.3.1.freebsd-amd64.tar.gz":           "99e23fdd33860d837912e8647ed2a4b3d2b09d3c",
	"https://storage.googleapis.com/golang/go1.3.1.linux-386.tar.gz":               "36f87ce21cdb4cb8920bb706003d8655b4e1fc81",
	"https://storage.googleapis.com/golang/go1.3.1.linux-amd64.tar.gz":             "3af011cc19b21c7180f2604fd85fbc4ddde97143",
	"https://storage.googleapis.com/golang/go1.3.1.windows-386.zip":                "64f99e40e79e93a622e73d7d55a5b8340f07747f",
	"https://storage.googleapis.com/golang/go1.3.1.windows-386.msi":                "df37e307c52fbea02070e23ae0a49cb869d54f33",
	"https://storage.googleapis.com/golang/go1.3.1.windows-amd64.zip":              "4548785cfa3bc228d18d2d06e39f58f0e4e014f1",
	"https://storage.googleapis.com/golang/go1.3.1.windows-amd64.msi":              "88c5d9a51a74c2846226a08681fc28cd3469cba0",
	"https://storage.googleapis.com/golang/go1.3.src.tar.gz":                       "9f9dfcbcb4fa126b2b66c0830dc733215f2f056e",
	"https://storage.googleapis.com/golang/go1.3.darwin-386-osx10.6.tar.gz":        "159d2797bee603a80b829c4404c1fb2ee089cc00",
	"https://storage.googleapis.com/golang/go1.3.darwin-386-osx10.8.tar.gz":        "bade975462b5610781f6a9fe8ac13031b3fb7aa6",
	"https://storage.googleapis.com/golang/go1.3.darwin-386-osx10.6.pkg":           "07e7142540558f432a8750eb6cb25d6b06ed80bb",
	"https://storage.googleapis.com/golang/go1.3.darwin-386-osx10.8.pkg":           "c908ecdb177c8a20abd61272c260b15e513f6e73",
	"https://storage.googleapis.com/golang/go1.3.darwin-amd64-osx10.6.tar.gz":      "82ffcfb7962ca7114a1ee0a96cac51c53061ea05",
	"https://storage.googleapis.com/golang/go1.3.darwin-amd64-osx10.8.tar.gz":      "8d768f10cd00e0b152490291d9cd6179a8ccf0a7",
	"https://storage.googleapis.com/golang/go1.3.darwin-amd64-osx10.6.pkg":         "631d6867d7f4b92b314fd87115e1cefadeeac2ab",
	"https://storage.googleapis.com/golang/go1.3.darwin-amd64-osx10.8.pkg":         "4e8f2cafa23797211fd13f3fa4893ce3d5f084c4",
	"https://storage.googleapis.com/golang/go1.3.freebsd-386.tar.gz":               "8afa9574140cdd5fc97883a06a11af766e7f0203",
	"https://storage.googleapis.com/golang/go1.3.freebsd-amd64.tar.gz":             "71214bafabe2b5f52ee68afce96110031b446f0c",
	"https://storage.googleapis.com/golang/go1.3.linux-386.tar.gz":                 "22db33b0c4e242ed18a77b03a60582f8014fd8a6",
	"https://storage.googleapis.com/golang/go1.3.linux-amd64.tar.gz":               "b6b154933039987056ac307e20c25fa508a06ba6",
	"https://storage.googleapis.com/golang/go1.3.windows-386.zip":                  "e4e5279ce7d8cafdf210a522a70677d5b9c7589d",
	"https://storage.googleapis.com/golang/go1.3.windows-386.msi":                  "d457a86ce6701bb96608e4c33778b8471c48a764",
	"https://storage.googleapis.com/golang/go1.3.windows-amd64.zip":                "1e4888e1494aed7f6934acb5c4a1ffb0e9a022b1",
	"https://storage.googleapis.com/golang/go1.3.windows-amd64.msi":                "e81a0e4f551722c7682f912e0485ad20a287f2ef",
	"https://storage.googleapis.com/golang/go1.2.2.src.tar.gz":                     "3ce0ac4db434fc1546fec074841ff40dc48c1167",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-386-osx10.6.tar.gz":      "360ec6cbfdec9257de029f918a881b9944718d7c",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-386-osx10.8.tar.gz":      "4219b464e82e7c23d9dc02c193e7a0a28a09af1a",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-386-osx10.6.pkg":         "dff27e94c8ff25301cd958b0b1b629e97ea21f03",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-386-osx10.8.pkg":         "f1fb44aa22cba3e81dc33f88393a54e49eae0d8b",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-amd64-osx10.6.tar.gz":    "24c182718fd61b2621692dcdfc34937a6b5ee369",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-amd64-osx10.8.tar.gz":    "19be1eca8fc01b32bb6588a70773b84cdce6bed1",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-amd64-osx10.6.pkg":       "2d4b49f1105a78e1ea31d7f9ea0b43909cc209be",
	"https://storage.googleapis.com/golang/go1.2.2.darwin-amd64-osx10.8.pkg":       "5d78f2a3fe82b01fe5dfcb267e703e754274b253",
	"https://storage.googleapis.com/golang/go1.2.2.freebsd-386.tar.gz":             "d226b8e1c3f75d31fa426df63aa776d7e08cddac",
	"https://storage.googleapis.com/golang/go1.2.2.freebsd-amd64.tar.gz":           "858744ab8ff9661d42940486af63d451853914a0",
	"https://storage.googleapis.com/golang/go1.2.2.linux-386.tar.gz":               "d16f892173b0589945d141cefb22adce57e3be9c",
	"https://storage.googleapis.com/golang/go1.2.2.linux-amd64.tar.gz":             "6bd151ca49c435462c8bf019477a6244b958ebb5",
	"https://storage.googleapis.com/golang/go1.2.2.windows-386.zip":                "560bb33ec70ab733f31ff15f1a48fe35963983b9",
	"https://storage.googleapis.com/golang/go1.2.2.windows-386.msi":                "60b91a7bf68596b23978acb109d1ff8668b7d18f",
	"https://storage.googleapis.com/golang/go1.2.2.windows-amd64.zip":              "9ee22fe6c4d98124d582046aab465ab69eaab048",
	"https://storage.googleapis.com/golang/go1.2.2.windows-amd64.msi":              "c8f5629bc8d91b161840b4a05a3043c6e5fa310b",
	"https://storage.googleapis.com/golang/go1.4rc2.src.tar.gz":                    "270afd320c0b8e3bfa6f5e3b09e61a3917489494",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-386-osx10.6.tar.gz":     "98c80b0c30b5ccb48e02b5ed0d4c5047db82fa4f",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-386-osx10.8.tar.gz":     "e6b6f970f4c487c256e35e1aa1bd7c4ff0f74cf3",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-386-osx10.6.pkg":        "40edb3f2000a3dba73062545794fa3f709c827ad",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-386-osx10.8.pkg":        "d8d445c949dae30e29a95de94eadc8758d48061a",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-amd64-osx10.6.tar.gz":   "1a8649c1cd13c13dc7820ae02ee2abac3856dd70",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-amd64-osx10.8.tar.gz":   "3769cc4a72cff59f3a3ce3a9d6309d999a749093",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-amd64-osx10.6.pkg":      "9e6e6215cb961dd9d479785299f26fdf79d20970",
	"https://storage.googleapis.com/golang/go1.4rc2.darwin-amd64-osx10.8.pkg":      "ee6aeec11be5ac15b0206c74293f1173bb3c7a14",
	"https://storage.googleapis.com/golang/go1.4rc2.freebsd-386.tar.gz":            "a9ebddfce542d82fdd9f27747ec5d5465470ef5d",
	"https://storage.googleapis.com/golang/go1.4rc2.freebsd-amd64.tar.gz":          "3f39c414c006baefaa5ac29f0cd7614e6bb010f3",
	"https://storage.googleapis.com/golang/go1.4rc2.linux-386.tar.gz":              "04d2c1d744ebb419738f4a2543621eee46223d91",
	"https://storage.googleapis.com/golang/go1.4rc2.linux-amd64.tar.gz":            "950f74edbee7e55f4ca5760c19f51fe24de8d05f",
	"https://storage.googleapis.com/golang/go1.4rc2.windows-386.zip":               "8be5274637c7b6d0b1305fc09c5ed5dcf5e58188",
	"https://storage.googleapis.com/golang/go1.4rc2.windows-386.msi":               "6571af0e97947412f449571dd143a0f3d9827f9f",
	"https://storage.googleapis.com/golang/go1.4rc2.windows-amd64.zip":             "6c37566760d6ca0482a9840d69578bc45e6029da",
	"https://storage.googleapis.com/golang/go1.4rc2.windows-amd64.msi":             "6ac7e8d99e58be1ba63be1330e1b4e7e438f14b7",
	"https://storage.googleapis.com/golang/go1.4rc1.src.tar.gz":                    "ff8e7d78e85658251a36e45f944af70f226368ab",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-386-osx10.6.tar.gz":     "127565a471073b4872745583a215f9c89a740686",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-386-osx10.8.tar.gz":     "f3d610c65a078f59fff46a00b34297d05b36aacd",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-386-osx10.6.pkg":        "7496637aec61a824398f142e23faa283ab0caa45",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-386-osx10.8.pkg":        "1c0f99bdfc82a8333ada5916b38d6d71387d282b",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-amd64-osx10.6.tar.gz":   "ae22ddca700ec2ef1f4933f5e13dbdd5149cc6c3",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-amd64-osx10.8.tar.gz":   "957b9d55f696bd078e74969bc1d1b43ca545069a",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-amd64-osx10.6.pkg":      "31be1cdb25a9707291cad9900fc020c57587a0be",
	"https://storage.googleapis.com/golang/go1.4rc1.darwin-amd64-osx10.8.pkg":      "2f663489724a2716da5d1b77121ea171ea7a3a50",
	"https://storage.googleapis.com/golang/go1.4rc1.freebsd-386.tar.gz":            "481b6b74314a4cafade7366c1d2722e0e0fe401d",
	"https://storage.googleapis.com/golang/go1.4rc1.freebsd-amd64.tar.gz":          "87e10516627da751842f33a1acb34d5eb07d48a7",
	"https://storage.googleapis.com/golang/go1.4rc1.linux-386.tar.gz":              "77299d1791a68f7da816bde7d7dfef1cbfff71e3",
	"https://storage.googleapis.com/golang/go1.4rc1.linux-amd64.tar.gz":            "a5217681e47dd1a276c66ee31ded66e7bf4d41b7",
	"https://storage.googleapis.com/golang/go1.4rc1.windows-386.zip":               "c50859276251ac464c0a7453a8dc3b84c863cf4e",
	"https://storage.googleapis.com/golang/go1.4rc1.windows-386.msi":               "d36340f876d0180a81fbb8dc73258cff4650fe67",
	"https://storage.googleapis.com/golang/go1.4rc1.windows-amd64.zip":             "b8276e8e8f4a2134b9a1533a9b1b6f3fe797a579",
	"https://storage.googleapis.com/golang/go1.4rc1.windows-amd64.msi":             "4d97adbc6a3f6ae33a89a1cf63ad60e85b0733e3",
	"https://storage.googleapis.com/golang/go1.4beta1.src.tar.gz":                  "f2fece0c9f9cdc6e8a85ab56b7f1ffcb57c3e7cd",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-386-osx10.6.tar.gz":   "a360e7c8f1d528901e721d0cc716461f8a636823",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-386-osx10.8.tar.gz":   "d863907870e8e79850a7a725b398502afd1163d8",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-386-osx10.6.pkg":      "ee4d1f74c35eddbdc49e9fb01e86a971e1bb54a7",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-386-osx10.8.pkg":      "c118f624262a1720317105d116651f8fb4b80383",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-amd64-osx10.6.tar.gz": "ad8798fe744bb119f0e8eeacf97be89763c5f12a",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-amd64-osx10.8.tar.gz": "e08df216d9761c970e438295129721ec8374654a",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-amd64-osx10.6.pkg":    "831e95cc381cc1afd6c4bfa886e86790f1c96de6",
	"https://storage.googleapis.com/golang/go1.4beta1.darwin-amd64-osx10.8.pkg":    "dc0b5805ba117654dd95c84ce7872406380de3d5",
	"https://storage.googleapis.com/golang/go1.4beta1.freebsd-386.tar.gz":          "65045b7a5d2a991a45b1e86ad11252bc84043651",
	"https://storage.googleapis.com/golang/go1.4beta1.freebsd-amd64.tar.gz":        "42fbd5336437dde85b34d774bfed111fe579db88",
	"https://storage.googleapis.com/golang/go1.4beta1.linux-386.tar.gz":            "122ea6cae37d9b62c69efa3e21cc228e41006b75",
	"https://storage.googleapis.com/golang/go1.4beta1.linux-amd64.tar.gz":          "d2712acdaa4469ce2dc57c112a70900667269ca0",
	"https://storage.googleapis.com/golang/go1.4beta1.windows-386.zip":             "a6d75ca59b70226087104b514389e48d49854ed4",
	"https://storage.googleapis.com/golang/go1.4beta1.windows-386.msi":             "1f8d11306d733bec975f2d747b26810926348517",
	"https://storage.googleapis.com/golang/go1.4beta1.windows-amd64.zip":           "386deea0a7c384178aedfe48e4ee2558a8cd43d8",
	"https://storage.googleapis.com/golang/go1.4beta1.windows-amd64.msi":           "ec3ec78072128d725878404a5ce27bd1c1e7132b",
	"https://storage.googleapis.com/golang/go1.3rc2.src.tar.gz":                    "53a5b75c8bb2399c36ed8fe14f64bd2df34ca4d9",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-386-osx10.6.tar.gz":     "600433eccda28b91b2afe566142bce759d154b49",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-386-osx10.8.tar.gz":     "36fa30bfdeb8560c5d9ae57f02ec0cdb33613cb5",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-386-osx10.6.pkg":        "c41c4e55017d3d835cf66feaaf18eeaeabaa066a",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-386-osx10.8.pkg":        "4cbdcccac38eed1ebffbbf1eba594724e5d05a77",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-amd64-osx10.6.tar.gz":   "84c25957096d4700f342c10f82f1f720bf646f6e",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-amd64-osx10.8.tar.gz":   "1bd241130b5e7a3eb4876fbb17257b16ea9db67d",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-amd64-osx10.6.pkg":      "18c8ed409a7ba97a61e00f00982361d7c84f7fdb",
	"https://storage.googleapis.com/golang/go1.3rc2.darwin-amd64-osx10.8.pkg":      "33d2129c26ea0cb5f147fa5f24395c8ed5c4433f",
	"https://storage.googleapis.com/golang/go1.3rc2.freebsd-386.tar.gz":            "3e5394e0f4eb99c32510dda48eb4dc1af9717a41",
	"https://storage.googleapis.com/golang/go1.3rc2.freebsd-amd64.tar.gz":          "bbaba53742cf43d96abca710cf49fe8c0ede6673",
	"https://storage.googleapis.com/golang/go1.3rc2.linux-386.tar.gz":              "7462cb654712ef6785ccae5b75ed393de6f49da2",
	"https://storage.googleapis.com/golang/go1.3rc2.linux-amd64.tar.gz":            "3a7d86a3245b4c8bd4dc5b1ff4e0073c2d1b81b5",
	"https://storage.googleapis.com/golang/go1.3rc2.windows-386.zip":               "f3f7a995baf77742b813723bc823d584466cb26f",
	"https://storage.googleapis.com/golang/go1.3rc2.windows-386.msi":               "a1138a6f7d22768eac73dfb254a1af8531aaeb1b",
	"https://storage.googleapis.com/golang/go1.3rc2.windows-amd64.zip":             "607b6ed4830785d166d83029a76e6975b2e99068",
	"https://storage.googleapis.com/golang/go1.3rc2.windows-amd64.msi":             "dba588d51f9b9353c7bdc271cecea065eea06250",
	"https://storage.googleapis.com/golang/go1.3rc1.src.tar.gz":                    "6a9dac2e65c07627fe51899e0031e298560b0097",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-386-osx10.6.tar.gz":     "a15031c21871d9ffb567c7d204653b32f0d84737",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-386-osx10.8.tar.gz":     "7ace88dfe731c38e83cee27f23eb2588419cf249",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-386-osx10.6.pkg":        "8de0f308c51cec5fcec45fde762967723ef61eb9",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-386-osx10.8.pkg":        "498e0840c44258e6b29eb5aa34b2fb3c31e79fdd",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-amd64-osx10.6.tar.gz":   "d250f20d84c310aa82053dea16743b223bbf933a",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-amd64-osx10.8.tar.gz":   "e3fb91fcfa2dfa97e451de9048ec5788713bc94e",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-amd64-osx10.6.pkg":      "02e6537c9a3f0cc80dcf901b40683eeab6d8bebf",
	"https://storage.googleapis.com/golang/go1.3rc1.darwin-amd64-osx10.8.pkg":      "17c42d6e6b5ca99fcd1e6927a79652d7e630a226",
	"https://storage.googleapis.com/golang/go1.3rc1.freebsd-386.tar.gz":            "953a95277ef06da98f0b8d7bb9bd02f4846374ff",
	"https://storage.googleapis.com/golang/go1.3rc1.freebsd-amd64.tar.gz":          "3c0a03ee5a64f6db46298fa3ad26d577ef7b2db5",
	"https://storage.googleapis.com/golang/go1.3rc1.linux-386.tar.gz":              "07c656173c444e4373a799141c1cb28128a345eb",
	"https://storage.googleapis.com/golang/go1.3rc1.linux-amd64.tar.gz":            "affaccfd69a694e0aa59466450e4db5260aeb1a3",
	"https://storage.googleapis.com/golang/go1.3rc1.windows-386.zip":               "d43c973adede9e8f18118a2924d8b825352db50a",
	"https://storage.googleapis.com/golang/go1.3rc1.windows-386.msi":               "23534cce0db1f8c0cc0cf0f70472df59ac26bbfa",
	"https://storage.googleapis.com/golang/go1.3rc1.windows-amd64.zip":             "312358b64711fd827f9dfb0cef61383f9eb5057b",
	"https://storage.googleapis.com/golang/go1.3rc1.windows-amd64.msi":             "d089fbe3c12b8ec8d3e30526b3eb604c9ae84c7d",
}
