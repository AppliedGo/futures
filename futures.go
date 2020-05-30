/*
<!--
Copyright (c) 2019 Christoph Berger. Some rights reserved.

Use of the text in this file is governed by a Creative Commons Attribution Non-Commercial
Share-Alike License that can be found in the LICENSE.txt file.

Use of the code in this file is governed by a BSD 3-clause license that can be found
in the LICENSE.txt file.

The source code contained in this file may import third-party source code
whose licenses are provided in the respective license files.
-->

<!--
NOTE: The comments in this file are NOT godoc compliant. This is not an oversight.

Comments and code in this file are used for describing and explaining a particular topic to the reader. While this file is a syntactically valid Go source file, its main purpose is to get converted into a blog article. The comments were created for learning and not for code documentation.
-->

+++
title = "Futures in Go, no package required"
description = "With goroutines and channels, implementing Futures in Go is trivial."
author = "Christoph Berger"
email = "info@appliedgo.net"
date = "2020-05-16"
draft = "true"
categories = ["Concurrent Programming"]
tags = ["future", "goroutine", "channel"]
articletypes = ["Tutorial"]
+++

Futures are mechanisms for decoupling a value from how it was computed. Goroutines and channels allow modeling futures trivially. Does this approach cover all aspects of a future?

<!--more-->

## Back to the futures


Recently I came across a short comment on Reddit:

> peterbourgon (7.00/0.00): Futures in Go, no package required:
>
>    ```
>    c := make(chan int)         // future
>    go func() { c <- f() }() // async
>    value := <-c             // await
>    ```

I got curious. Is this sufficient to model a future as known in other languages?


## Futures in a nutshell

According to [Wikipedia](https://en.wikipedia.org/wiki/Futures_and_promises), futures *"describe an object that acts as a proxy for a result that is initially unknown, usually because the computation of its value is not yet complete."* Sounds like a natural fit for Go's built-in concurrency. Let's pick Peter Bourgon's minimal code apart.

### Part 1: Define the communication channel.

```go
c := make(chan int)
````

Channel `c` is our future, a proxy for a result that may or may not be ready at the point of reading. Channel semantics cause the reader to block and wait until a value is available.

### Part 2: Compute the value asynchronously.

For this we set up and execute a goroutine. The main goroutine can continue doing other things until it needs a value from c.

Let's define the channel as a parameter to the goroutine. The actual call then receives the channel we defined above as an argument.

```go
go func(res chan<- int) {
    res <- f()
}(c)
```

### Part 3: retrieve the computed value.

Finally, at some point, the main goroutine requests the value. If the channel already contains a result, it can be read right away; otherwise, the statement blocks until the result is there.

```go
value := <-c
```

Easy enough, right?

Be careful, however. This code contains two implicit assumptions about how to compute and read a future.

**Assumption #1:** It is ok that the spawned goroutine blocks after having calculated the result.

**Assumption #2:** The reader reads the result only once.


### Let the spawned goroutine do more things

Assumption #1 allowed us to create a channel with zero length. The write operation `c <- f()` then blocks until a reader is ready to receive the value from the channel. This is perfectly fine if the goroutine's only job is to calculate the future's result. In most cases, this is exactly what we need.

If, for some reason, calculation of the future is embedded in a broader context that is supposed to continue to run concurrently, simply use a channel of length 1 to store the result until the reader is ready to retrieve it:

```go
c := make(chan int, 1)
```

Now the spawned goroutine can pass the result to the channel and continue immediately, maybe computing other futures that depend on the one just delivered. Or doing cleanup or whatever.

### Make the read operation idempotent

Assumption #2 is also just fine in most cases. However, what if a package API expose a future to clients? Other people's code is usually impossible to control, so we should take measures to ensure that the result can be safely read multiple times. (In other words, the read operation should be *idempotent*.)

Again, this is trivial in Go. We even have several approaches to choose from.

### Read only once with sync.Once

We can make use of the `sync` package here and define a `Once` function.

```go
var future int
o := sync.Once{}  // Create a Once struct
get := func() int {
	o.Do(func() { future = <-c }) // This func is called only once for the lifetime of `o`.
	return future
}
```

To be honest, I do not like this approach and would not use it myself.

Two reasons:
1. One more dependency on a package (albeit from the standard library)
2. The trickery around returning the value

Luckily, there is another approach available.

### Provide the computed value over and over again

When the calculating goroutine provides the result as often as we need it, then we can keep the receiving side as simple as in the original approach.

The change to the goroutine is also very simple. We only need to add an infinite loop for feeding the result to the channel.

```go
go func(res chan<- int) {
	// calculate...
	for {
		res <- 4096
	}
}(c)
```

On the consuming side, the code is again as easy as reading from a channel.

```go
value = <-c
value = <-c  // Read again, get the same value again
```

### Close a channel when work is done

The technique we use here is a Go idiom that makes use of the fact that a closed channel dispenses the zero value of its element type on every read attempt. Here is how it works:

1. An unbuffered channel is shared between sender and receiver.
2. The reader attempts to read from the channel and blocks.
3. When the sender is ready to share the result, it simply closes the channel.
4. The closed channel dispenses a zero value. This unblocks the reader.
5. Now the reader can read the calculated value.
6. If the reader tests the channel again, it will not block as the channel continues to dispense the zero value.

Using this concept, our future looks as follows.

```go

```

## The code
*/

// ## Imports and globals
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	// Create an unbuffered channel.
	c := make(chan int)

	fmt.Println("\nA simple future")
	fmt.Println("---------------\n")

	// This goroutine receives a channel, does some calculation, and writes the result to the channel.
	go func(res chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		fmt.Println("done")
		res <- 1024
	}(c)

	// Read the future by receiving its value from the channel.
	var value int
	fmt.Println("Waiting")
	// This call blocks until a value is available.
	value = <-c
	fmt.Println("got", value)

	fmt.Println("\nMultiple reads with sync.Once")
	fmt.Println("-----------------------------\n")

	// `sync.Once{}` provides a method `Do()` that receives a function and executes the function only once. Subsequent calls to the same function only return a cached result.
	o := sync.Once{}

	// `Do()` does not return anything, so we need a variable to store the result of the call.
	var future int

	// `get()` wraps the call to `Do()`.
	get := func() int {
		// `Do()` calls an anonymous func that reads the future from channel c.
		o.Do(func() { future = <-c })
		// Trick return! This function acts like a closure and therefore has access to the outer scope.
		return future
	}

	// The calculating goroutine is the same as before.
	go func(res chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		fmt.Println("Writing result")
		res <- 2048
	}(c)

	// Instead of reading the channel, we now call `get()`.
	fmt.Println("Waiting")
	value = get()
	fmt.Println("got", value)
	value = get()
	fmt.Println("got", value)

	fmt.Println("\nMultiple reads from a channel that never dries up")
	fmt.Println("-------------------------------------------------\n")

	// We modify the calculating goroutine a bit. After calculating the result, the goroutine goes into an infinite loop to pass the result to the channel as often as some other code wants to read it. Note that this is not a busy loop, as each iteration blocks until the channel is free to write to.
	go func(res chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		for {
			fmt.Println("Writing result")
			res <- 4096
		}
	}(c)

	// Now we can read repeatedly from the channel and get the same result as often as we want.
	fmt.Println("Waiting")
	fmt.Println("got", <-c)
	fmt.Println("got", <-c)
	//	fmt.Println("got", <-c)

	fmt.Println("\nUsing a 'done' channel")
	fmt.Println("----------------------\n")

	// Create a channel only for the purpose of closing it. The type can be anything.
	done := make(chan bool)

	// Can you imagine why we need this Sleep() call here?
	time.Sleep(1 * time.Millisecond)

	// The calculating goroutine receives the done channel, writes the result directly into `value`, and closes the channel.
	// In general, having a goroutine write into a value defined outside is really bad practice, as it is prone to race conditions if multiple goroutines can write to that variable. However, here we definitely have only one goroutine, so for the sake of simplicity I'll leave it that way.
	go func(d chan<- bool) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		fmt.Println("Writing result")
		value = 8192
		close(d)
	}(done)

	// Readers must test the `done` channel before accessing the value.
	fmt.Println("Waiting")
	<-done
	fmt.Println("got", value)
	<-done
	fmt.Println("got", value)

}

/*
## How to get and run the code

Step 1: `go get` the code. Note the `-d` flag that prevents auto-installing
the binary into `$GOPATH/bin`.

    go get -d github.com/appliedgo/TODO:

Step 2: `cd` to the source code directory.

    cd $GOPATH/src/github.com/appliedgo/TODO:

Step 3. Run the binary.

    go run TODO:.go


## Odds and ends
## Some remarks
## Tips
## Links


**Happy coding!**

*/
