# Get sign benchmark for bitxhub

test bitXHub get sign benchmark.

## Getting started

- you can **clone** the project by

``` shell
git clone git@github.com:Karenlrx/benchmarkForBxh.git
```
- modify config
    - you should ensure start bitxhub correctly,
    - and ensure you have store some ibtp data in bitxhub,
    ``` shell
    vim main.go
    # modify the interchain value which you store in bitxhub
    var interchain = "1356:eth1:0x056401B3E8e357e7C4ABDC0569a04447a01e1402-1356:eth2:0xb7bCC3F7f995A17c8C07B01deb66b2dFa0CFFDf5-1"
    ```
    - then modify ip and port, modify getSign interface input of id.
- compile project
``` shell
cd benchmarkForBxh && go build -o getSign *.go 
```

- run test:
``` shell
./getSign -g 1 -r 1 -size 10 -t multiSign -need_retry=false -mock_pier=false
```
    you can get the help of command:
``` shell
Usage of ./getSign:
  -g int
        The number of concurrent go routines to get mutiSign from bitxhub (default 200)
  -mock_pier
        mock pier to get multiSign
  -need_retry
        retry query for mock pier flag
  -r int
        The round of concurrent go routines to get mutiSign from bitxhub (default 100)
  -size int
        The size in grpc client pool (default 200)
  -t string
        The sign type (default "multiSign")
```

## For example
![img.png](asserts/getSign.png)