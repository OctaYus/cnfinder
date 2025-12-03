CnFinder is a tool for extracting CNAMEs within list of targets/subdomains.


## Installation

1. Use this go command for an easy installation:

```bash
go install github.com/OctaYus/cnfinder@latest
```

2. If you wanna build it yourself use this instead:

```bash
git clone https://github.com/OctaYus/cnfinder.git
cd cnfinder/cmd/cnfinder
go build -o main main.go
```

3. Usage help:
   
```bash
Usage of cnfinder:
  -a    append to output instead of truncating
  -l string
        input file with one subdomain per line, or '-' for stdin
  -o string
        output file (each line: domain > cname) (default "cnames.txt")
  -t int
        number of concurrent workers (default: CPUs) (default 12)
  -timeout duration
        DNS query timeout, e.g. 3s, 500ms (default 5s)

Examples:
  cnfinder -l subdomains.txt -o results.txt
  cat subdomains.txt | cnfinder -l - -o results.txt

```

## Contribute  

Found a bug? Got a killer feature idea?  
- Open an **issue**  
- Send a **pull request**  
- No BS—just practical improvements  

## License  

**MIT License** - Do what you want, just don't blame us.  
