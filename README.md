# HTTP Server for computing prime factors

This is a server written in Go that computes the prime factor decomposition of integers.

## Launch the server

Start by cloning the repository:

```
git clone github.com/r0mainK/prime-server
```

Go in the cloned directory, then run the following command to build the Docker image:

```
docker build -t prime-server .
```

Once the image is built, launch the server with the following command:

```
docker run -d -p 8080:8080 prime-server
```

The server is now up and running, and exposed through port 8080 and endpoint `/query`! By default, up to 100 requests can be processed by the server simultaniously. Request will be blocked once this limit is hit, regardless of the query, until an available slot s freed. If you wish to modify this limit to `k` requests, you can do so when launching the server, by running:

```
docker run -d -p 8080:8080 prime-server k
```

## Query the server

In order to get the prime factors of a given integer `i` simply send a request to it like so:

```
curl http://localhost:8080/query?n=i
```

Once all prime factors have been computed, you will receive a JSON file where each key is a prime factor, and each value is it's multiplicity. For example, if `n=20`, you will get the following response:

```
{
    "2": 2
    "5": 1
}
```

_The keys are not integers because it is not allowed in the JSON format._

## Implementation details

This server has been implemented in order to handle gracefully large amounts of requests. Given a request, the list of known primes will first be iterated upon. If the decomposition is attained, the result will be sent and resources freed. If not, the server will start looking for new primes with a relatively optimized version of the sieve of Eratosthenes, stopping once the decomposition is attained. All primes computed are stored, in order to avoid repeating looking for them multiple times - however we do not store previous decompositions. In order to maximize the amount of requests which can be handled, we do not look for new primes in a parallel fashion. This means that if multiple requests are waiting for new primes, they will do so synchronously, however this will not affect other requests (unless the limit has been hit, and all requests need new primes).
