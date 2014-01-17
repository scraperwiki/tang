Tang
====


![Ancanthurus leucosternon](http://upload.wikimedia.org/wikipedia/commons/thumb/3/36/Acanthurus_leucosternon_01.JPG/640px-Acanthurus_leucosternon_01.JPG "Powder Blue Tang")

Tang installs itself as a github service hook, then listens. It
does stuff when you push to `github.com`. Add a `tang.hook` file
to your repo and tang will run it when pushed, lighting up your
github repos with red, green, and amber lights.

When deployed at Scraperwiki, its available on `services.scraperwiki.com`.

`go` is required.

# Running tang

The source code for tang must be installed in a
place where `go install` can find it, probably underneath your
`GOPATH`. I have a symlink made like this:

    mkdir -p $GOPATH/src/github.com
    ln -s ~/sw $GOPATH/src/github.com/scraperwiki

Docker must be installed and a docker container for tang must be
built:

    docker build -t tang .

A master script will build tang, install it, and run it in a
docker container (building and installing tang is quick enough that we
do it most of the time):

    # Set GITHUB_USER and GITHUB_PASSWORD
    . ./github-password.sh
    sudo ./start-tang


# Building, Installing, Testing

    go build            # builds

    ./install-tang      # builds and installs

    go test             # tests

    tang                # runs tang
    sudo -E ./tang      # runs tang as root
    ./tang --help       # lists options

# Principles of Operation

tang listens (on port 8080 by default, but we expect this to be
fronted by nginx or similar).

It responds to various signals and conditions (typical of most
Unix daemons):

* `SIGQUIT` - quits.
* `SIGINT` and `SIGHUP` - restart by reloading the executable.
* `EOF` (on stdin) - quits an interactive tang.

The URLs tang responds to are:

* `/hook` - for handling calls from github.com (checks out repo and
  runs `tang.hook`).

* `/tang/logs` - serve the log directory

* `/tang` - for experimentation and testing

URLs in the domain `qa.scraperwiki.com` are routed to a server
built by a repos' `tang.serve` script (if the repo has one). The
`tang.serve` script will be run on demand in a docker container
that tang creates.

`[<params>].<branch>.<repo-name>.qa.scraperwiki.com` will route to the tag
`<branch>` on the repo `scraperwiki/<repo-name>`. `<params>` is
optional and will be made available to the server for it to use
as configuration parameters.


Roadmap
=======

- ✓ On git push, update a local clone, if tang.hook exists check it out and invoke tang.hook.
- ✓ (for now) Only run for 'allowed pushers'
- ✓ Tang runs inside a docker container
- tang-event script for synthesizing github events
- Have an interface for "starting" and "stopping"
- Provide a persistent data volume (assume we can trust tang.hook for now, later we can have auth by repository)
- Tang runs tang.hook inside docker containers

### Credits

Image by H. Zell used under GFDL:
http://en.wikipedia.org/wiki/File:Acanthurus_leucosternon_01.JPG
