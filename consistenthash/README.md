# Current implementation

```bash
$ go test -bench=. ./...
goos: linux
goarch: amd64
pkg: github.com/modernprogram/groupcache/v2/consistenthash
cpu: 13th Gen Intel(R) Core(TM) i7-1360P
BenchmarkGet8-16      	27974676	        37.06 ns/op
BenchmarkGet32-16     	29410753	        39.68 ns/op
BenchmarkGet128-16    	25937828	        45.28 ns/op
BenchmarkGet512-16    	19554933	        54.32 ns/op
PASS
ok  	github.com/modernprogram/groupcache/v2/consistenthash	4.769s
```

# Previous implementation

```bash
$ go test -bench=. ./...
goos: linux
goarch: amd64
pkg: github.com/modernprogram/groupcache/v2/consistenthash
cpu: 13th Gen Intel(R) Core(TM) i7-1360P
BenchmarkGet8-16      	27004227	        43.38 ns/op
BenchmarkGet32-16     	24552079	        47.78 ns/op
BenchmarkGet128-16    	21309058	        53.39 ns/op
BenchmarkGet512-16    	14953760	        71.02 ns/op
PASS
ok  	github.com/modernprogram/groupcache/v2/consistenthash	5.747s
```
