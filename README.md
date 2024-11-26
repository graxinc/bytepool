# bytepool

[![Go Reference](https://pkg.go.dev/badge/github.com/graxinc/bytepool.svg)](https://pkg.go.dev/github.com/graxinc/bytepool)

## Purpose

Allocations getting you down?

- Centralized byte pool interface
- Optimizations/additions on existing pool packages
- Support for variable and constant sizes

## Usage

If you can choose some size bounds, prefer:
```
pool := bytepool.NewBucket(10, 100_000)

b := pool.Get()
defer b.Put(b)

// use buffer
b.B = append(b.B, 1,2,3) 
writeData(b.B)
```

If you don't know any bounds:
```
pool := bytepool.NewDynamic()

// use
```

If the buffer is usually one size:
```
pool := bytepool.NewSync()

// use
```
