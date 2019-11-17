package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"golang.org/x/sync/semaphore"
)

const defaultQueryLimit = 100

var queryLimit int
var querySemaphor semaphore.Weighted

var knownPrimes = []int{2}
var knowPrimesMutex = sync.RWMutex{}

var primeSenderBus []chan int
var senderMutex = sync.Mutex{}

var computeMutex = sync.Mutex{}
var isComputing bool

var sieve = make(map[int]struct{})
var lowerLimit int
var upperLimit = 2

type primeFactors map[int]int

func removeMultiples(p, lowerLimit, upperLimit int, sieve *map[int]struct{}) {
	m := lowerLimit / p
	for m*p <= upperLimit {
		if _, ok := (*sieve)[m*p]; ok {
			delete((*sieve), m*p)
		}
		m++
	}
}

func computeNextPrime(newUpperLimit int) {
	// Lock the computing so next computation must wait until sieve is ready
	computeMutex.Lock()
	defer computeMutex.Unlock()
	if !(len(sieve) > 0) {
		// Create sieve if necessary, taking previous upper limit as new low
		lowerLimit = upperLimit + 1
		if lowerLimit%2 == 0 {
			lowerLimit++
		}
		upperLimit = newUpperLimit
		// All odd integers between the lower and upper limit are added to the sieve
		for p := lowerLimit; p <= upperLimit; p += 2 {
			sieve[p] = struct{}{}
		}
		// Primes are copied so multiples can be removed without impacting other goroutines
		knowPrimesMutex.RLock()
		primes := make([]int, len(knownPrimes))
		copy(primes, knownPrimes)
		knowPrimesMutex.RUnlock()
		// Multiples of known primes are removed from the sieve
		for _, p := range primes {
			removeMultiples(p, lowerLimit, upperLimit, &sieve)
		}
	}
	// Iteration through the sieve until a prime is found
	for p := lowerLimit; p <= upperLimit; p += 2 {
		if _, ok := sieve[p]; ok {
			// Wait for write access to write prime
			knowPrimesMutex.Lock()
			log.Printf("found new prime: %d", p)
			newKnownPrimes := make([]int, len(knownPrimes)+1)
			copy(newKnownPrimes, knownPrimes)
			newKnownPrimes[len(knownPrimes)] = p
			knownPrimes = newKnownPrimes
			// Allow next computation, then send prime to listeners and empty the bus
			isComputing = false
			for _, primeSender := range primeSenderBus {
				primeSender <- p
			}
			primeSenderBus = make([]chan int, 0)
			knowPrimesMutex.Unlock()
			// Clean the sieve
			removeMultiples(p, p, upperLimit, &sieve)
			// Update the lower limit if possible
			for i := p + 2; i <= upperLimit; i += 2 {
				if _, ok := sieve[i]; ok {
					lowerLimit = i
					return
				}
			}
			return
		}
	}
}

func getNextPrime(primeIndex, upperLimit int, primeSender chan int) {
	// Lock the primes so no writing can occur
	knowPrimesMutex.RLock()
	defer knowPrimesMutex.RUnlock()
	// Check if the prime exists, and send it if that's the case
	if primeIndex < len(knownPrimes) {
		primeSender <- knownPrimes[primeIndex]
		return
	}
	senderMutex.Lock()
	defer senderMutex.Unlock()
	if !isComputing {
		// Start computing next prime
		isComputing = true
		go computeNextPrime(upperLimit)
	}
	// Add primeSender to the bus
	newPrimeSenderBus := make([]chan int, len(primeSenderBus)+1)
	copy(newPrimeSenderBus, primeSenderBus)
	newPrimeSenderBus[len(primeSenderBus)] = primeSender
	primeSenderBus = newPrimeSenderBus
}

func createFactorsList(w http.ResponseWriter, r *http.Request) {
	// Wait for slot
	ctx := r.Context()
	if err := querySemaphor.Acquire(ctx, 1); err != nil {
		log.Printf("request canceled while waiting for slot: %s\n", err)
		return
	}
	defer querySemaphor.Release(1)
	// Parse query
	query := r.URL.Query()
	s := query.Get("n")
	n, err := strconv.Atoi(s)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Printf("failed to process request: %s\n", err)
		return
	}
	// Compute factor decomposition
	pf := primeFactors{}
	primeReceiver := make(chan int, 1)
	primeIndex := 0
	for n != 1 {
		go getNextPrime(primeIndex, n, primeReceiver)
		select {
		case p := <-primeReceiver:
			m := 0
			for n%p == 0 {
				m++
				n /= p
			}
			if m > 0 {
				pf[p] = m
			}
		case <-ctx.Done():
			log.Printf("request canceled while computing: %s\n", ctx.Err())
			return
		}
		primeIndex++
	}
	// Write response
	js, err := json.Marshal(pf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("failed to create reponse: %s\n", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func main() {
	// Check args
	if l := len(os.Args) - 1; l > 1 {
		log.Fatalf("expected 0 or 1 argument, got %d\n", l)
	} else if l == 1 {
		ql, err := strconv.Atoi(os.Args[1])
		if err != nil {
			log.Fatalf("could not convert %s to an integer\n", os.Args[1])
		}
		queryLimit = ql

	} else {
		queryLimit = defaultQueryLimit
	}
	// Create semaphor restraining the maximum number of query
	querySemaphor = *semaphore.NewWeighted(int64(queryLimit))
	// Setup exit handling
	done := make(chan os.Signal)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGINT)
	go func() {
		<-done
		log.Fatalln("server stopped")
	}()
	log.Println("server started")
	// Start listening
	http.HandleFunc("/query", createFactorsList)
	http.ListenAndServe(":8080", nil)
}
