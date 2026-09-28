module github.com/devicelab-dev/maestro-runner

go 1.25.0

// Pinned so builds and govulncheck agree on a compiler that carries the
// stdlib security fixes. Policy: bump this whenever govulncheck flags the
// standard library — a permanently red gate teaches everyone to ignore it.
toolchain go1.26.7

require (
	github.com/urfave/cli/v2 v2.27.7
	golang.org/x/text v0.39.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/dop251/goja_nodejs v0.0.0-20260212111938-1f56ff5bcf14
	github.com/rogpeppe/go-internal v1.16.0 // indirect
)

require (
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/coder/websocket v1.8.15
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/danielpaulus/go-ios v1.0.131
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dop251/goja v0.0.0-20251201205617-2bb4c724c0f9
	github.com/go-rod/rod v0.116.2
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/google/pprof v0.0.0-20240727154555-813a5fbdbec8 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/miekg/dns v1.1.57 // indirect
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	github.com/ysmood/fetchup v0.2.3 // indirect
	github.com/ysmood/goob v0.4.0 // indirect
	github.com/ysmood/got v0.40.0 // indirect
	github.com/ysmood/gson v0.7.3 // indirect
	github.com/ysmood/leakless v0.9.0 // indirect
	go.mozilla.org/pkcs7 v0.0.0-20210826202110-33d05740a352 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
	howett.net/plist v0.0.0-20200419221736-3b63eb3a43b5 // indirect
	software.sslmate.com/src/go-pkcs12 v0.2.0 // indirect
)
