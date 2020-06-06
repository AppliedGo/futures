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
description = "How to trivially implement futures in Go using goroutines and channels"
author = "Christoph Berger"
email = "info@appliedgo.net"
date = "2020-06-06"
draft = "true"
categories = ["Concurrent Programming"]
tags = ["future", "goroutine", "channel"]
articletypes = ["Tutorial"]
+++

Futures are mechanisms for decoupling a value from how it was computed. Goroutines and channels allow implementing futures trivially. Does this approach cover all aspects of a future?

<!--more-->

## Back to the futures


Recently I came across a short comment on Reddit:

> peterbourgon (7.00/0.00): Futures in Go, no package required:
>
>    ```
>    c := make(chan int)      // future
>    go func() { c <- f() }() // async
>    value := <-c             // await
>    ```

I got curious. Is this sufficient to model a future as known in other languages? Or would advanced use cases still require a `futures` package for properly modeling futures semantics?


## Futures in a nutshell

According to [Wikipedia](https://en.wikipedia.org/wiki/Futures_and_promises), futures *"describe an object that acts as a proxy for a result that is initially unknown, usually because the computation of its value is not yet complete."* Sounds like a natural fit for Go's built-in concurrency. Let's pick Peter Bourgon's minimal code apart.

### Part 1: Define the communication channel.

```go
c := make(chan int)
````

Channel `c` is our future, a proxy for a result that may or may not be ready at the point of reading. Channel semantics cause the reader to block and wait until a value is available.

### Part 2: Compute the value asynchronously.

For this we set up and execute a goroutine. The main goroutine can continue doing other things until it needs the computed value from c.

Deviating from the original code, let's define the channel as a parameter to the goroutine. The actual call then receives the channel we defined above as an argument. We can also pass input parameters as needed.

```go
go func(input int, result chan<- int) {
    result <- input * 2 // Horribly complex and long-winded calculation
}(1, c)
```

### Part 3: retrieve the computed value.

Finally, at some point, the main goroutine requests the value. If the channel already contains a result, it is retrieved right away; otherwise, the statement blocks until the result is there.

```go
value := <-c
```




## A closer look

The above code is easy enough, right?

However, this code contains some implicit assumptions about how to compute and read a future.

**Assumption #1:** It is ok that the spawned goroutine blocks after having calculated the result.

**Assumption #2:** The reader reads the result only once.

**Assumption #3:** The spawned goroutine provides a result within a reasonable time.

All of these limiting assumptions can be addressed. And the best part is, the additional code required here is also trivial and makes use of well-known standard Go features.

Let's have a look at each of them.

### Let the spawned goroutine do more after computing the future

Assumption #1 allows us to create a channel with zero length. The write operation `c <- f()` then blocks until a reader is ready to receive the value from the channel. This is perfectly fine if the goroutine's only job is to calculate the future's result. In most cases, this is exactly what we need.

If, for some reason, calculation of the future is embedded in a broader context that is supposed to continue to run concurrently, simply use a channel of length 1 to store the result until the reader is ready to retrieve it:

```go
c := make(chan int, 1)
```

Now the spawned goroutine can pass the result to the channel and continue immediately, maybe computing other futures that depend on the one just delivered. Or doing cleanup or whatever.

However, here lies a catch: On a single-core CPU where the concurrent execution cannot be effectively parallelized, the computing goroutine may block the reading goroutine while it continues computing things.

### Read the computed future more than once

Assumption #2 is also just fine in most cases. However, sometimes you might have multiple goroutines that shall receive the computed value.

Again, this is trivial in Go. We only need to make the computing goroutine send the result to the channel over and over again.

```go
go func(input int, result chan<- int) {
	// calculate...
	for {
		result <- 4096
	}
}(c)
```

On the consuming side, noting needs to change. We can repeatedly reading from the channel and get the same value back, once it has been computed.

```go
value1 := <-c
value2 := <-c  // Read again, get the same value again
```

And in case you wonder -- no, there is no busy-looping happening here. The loop blocks on every attempt to write to the channel until a reader retrieves a value from the channel.



### Limit the time to wait for the future

In some cases it is better to not rely on the goroutine to provide a value in time. For example, the algorithm to compute the future might be of exponential time complexity, and the caller might have passed an input that causes the result to take ages to compute. Or the future might be calculated by calling a remote function over a slow and/or unreliable network.

Obviously, we need to be able to set an upper limit for the time to wait for the result. Again, this is quite easy: Go provides *contexts* to equip goroutines with a timeout.

This time we need to change the caller/reader side. The computing goroutine can remain unchanged.

To be able to watch for both the result of the future and a timeout, we define a `get()` function to retrieve the value. Inside the function, a `select` statement observes both the result channel and a timer that we start by calling `time.After()`. This method returns a channel upon calling, and sends the current time through that channel when the time is up. We do not need that time, so the result is discarded.

When the timer triggers, we need to indicate failure to the caller. For this, we can add a second return parameter that turns true if a timeout occurs.

```go
	get := func(s int) (result int, timedout bool) {
		select {
		case result = <-c3:
			return result, false
		case <-time.After(time.Duration(s) * time.Second):
			return 0, true
		}
	}
```

To retrieve the future, call `get()`, pass the desired timeout (in seconds), and test the boolean:

```go
value, timedOut := get(1)
if timedOut {
	...
}
```

## More "futureness"

Futures in other languages usually have a couple more methods as the authors strived to cover every imaginable use case. You do not need those at all costs. If you do, here are suggestions for mapping these methods to Go features.

Note that I have not added the code snippets from this section to the main code listing below. I feel that adding too much fine-grained control can easily lead to over-engineered code and tight coupling between goroutines. However, as there might be use cases, I discuss them here briefly and leave the implementation as an exercise for the reader.


### Cancel the computing goroutine

In some situations, a future may become obsolete before it has been fully computed. To save resources, the computing goroutine should be canceled then.

Here, the `context` package comes in handy. A context is an object that provides canceling, deadline, and timeout functionalities to goroutines. For canceling a goroutine, create a Background context and add the Cancel option.

```go
ctx, cancel := context.WithCancel(context.Background())
```

Pass the `ctx` object to the goroutine. The second return value, `cancel`,  is a function. When the goroutine is not needed, call this function to request the goroutine to cancel itself.

How does this work?

The context object contains a `done` channel. This channel delivers no values as long as it is open. Hence reading from the channel blocks the reader as long as the channel is open. Calling `cancel()` closes this channel. A closed channel starts delivering the zero value of its element type, and so any reader unblocks and can invoke code for cleaning up and exiting the goroutine.

To implement this inside the computing goroutine, execute a select statement in a loop. Make the select watch for the `done` channel to get closed. As long as the `done` channel is open, any read operation blocks because the `done` channel does not deliver anything. Hence the select statement skips this case block and evaluates other `case` blocks instead.

```go
go func(ctx context.Context) {
	// compute the future
	for {
		select {
		case <-ctx.Done():
			// cleanup code here
			return
		case default:
			// compute the future
		}
	}
}(ctx)
```

This concept can be extended to multiple goroutines that compute the same future. When the fastest computation finishes, all other computing goroutines can be canceled via the `done` channel.


## Have the computing goroutine time out

The above mechanism can as well be used for having the computing goroutine time out. This is an improved version of the above approach, where we only unblocked the reader after the time out. With a context, we can cancel the computing goroutine itself and thus stop it from further consuming CPU time or other resources. The `WithTimeout()` method of a `Context` object creates the same `done` channel as the `WithCancel()` method, and even returns a `cancel` function that can be called, but in addition, the `done` channel is also closed when the passed-in time has elapsed.

```go
ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
```

### More control?

There are even more ways of interacting with the computation of a future. For example, [jQuery's Deferred Object](https://api.jquery.com/category/deferred-object/) provides methods for chaining, notifications, progress notifications, state inspection, and other bells and whistles. If you have a closer look at them, I am sure you will find ways of implementing these methods via channels, waitGroups, or contexts.

However, as said above, don't over-engineer your code. If you feel the need to have your goroutines micro-manage each other using a truckload of methods for inspection and manipulation, this might be a good opportunity to re-think your overall concurrency design. Chances are that you will find a cleaner way that is more manageable and in the end also easier to reason about.

And back to my initial question: Would you be better off with a futures package? I see two possible reasons for using a package rather than native Go features: convenience, and domain-specific semantics. A convenience package can provide methods and objects that might help you switching from some other language to Go. And if you develop code for a specific problem domain, then a domain-specific API can "talk" to you in the language of that problem domain and avoid having to jump between different levels of abstraction. Neither of the two reasons is really life-saving, so it is a matter of personal (or team) preference whether to use a package or to stick to native Go features. Tip: start with the latter and only switch to a convenience or domain-specific package if they help managing complexity in your specific situation.

## The code

The code below contains all of the above in one single, executable file. Feel free to use it for your own experiments.
*/

// ## The basics
package main

import (
	"fmt"
	"time"
)

func main() {
	// Create an unbuffered channel.
	c := make(chan int)

	fmt.Println("\nA simple future")
	fmt.Println("---------------\n")

	// This goroutine receives a channel, does some calculation, and writes the result to the channel.
	go func(input int, result chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		fmt.Println("done")
		result <- input * 2
	}(1, c)

	// Read the future by receiving its value from the channel.
	var value int
	fmt.Println("Waiting")
	// This call blocks until a value is available.
	value = <-c
	fmt.Println("got", value)

	// ## Read the future multiple times

	fmt.Println("\nReading the future multiple times")
	fmt.Println("-----------------------------\n")

	c2 := make(chan int)

	// We modify the calculating goroutine a bit. After calculating the result, the goroutine goes into an infinite loop to pass the result to the channel as often as some other code wants to read it. Note that this is not a busy loop, as each iteration blocks until the channel is free to write to.
	go func(input int, res chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(1 * time.Second)
		for {
			fmt.Println("Writing result")
			res <- input * 4
		}
	}(1, c2)

	// Now we can read repeatedly from the channel and get the same result as often as we want.
	fmt.Println("Waiting")
	fmt.Println("got", <-c2)
	fmt.Println("got", <-c2)

	// ## Read with a timeout

	fmt.Println("\nReading with a timeout")
	fmt.Println("-----------------------------\n")

	c3 := make(chan int)

	// The computing goroutine can remain unchanged.
	go func(input int, res chan<- int) {
		fmt.Println("Calculating")
		time.Sleep(2 * time.Second)
		for {
			fmt.Println("Writing result")
			res <- input * 8
		}
	}(1, c3)

	// The select statement allows reading from multiple channels simultaneously. Here, we use it to block until either the future is ready to read or the timer triggers, whichever happens first.
	get := func(s int) (result int, timedout bool) {
		select {
		case result = <-c3:
			return result, false
		case <-time.After(time.Duration(s) * time.Second):
			return 0, true
		}
	}

	fmt.Println("Waiting")
	result, timedOut := get(1)
	if timedOut {
		// Handle the timeout.
		fmt.Println("Timed out")
		return
	}
	fmt.Println(result)
}

/*
## How to get and run the code

Step 1: clone the repository.

    git clone github.com/appliedgo/futures

Step 2: `cd` to the source code directory and run the code.

    go run futures.go

Or run the code directly in the [Go Playground](https://play.golang.org/p/gyLQb5mMKl_V).

**Happy coding!**

*/
