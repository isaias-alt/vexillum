// Command floor does nothing. It is the baseline the benchmark harness times
// next to vx, so the cost of starting any Go process on this machine (fork,
// exec, runtime init) can be told apart from the cost of vx itself.
package main

func main() {}
