Tang
====


![Ancanthurus leucosternon](http://upload.wikimedia.org/wikipedia/commons/thumb/3/36/Acanthurus_leucosternon_01.JPG/640px-Acanthurus_leucosternon_01.JPG "Powder Blue Tang")

Tang installs itself as a github service hook, then listens. It
does stuff when you push to `github.com`. Add a `tang.hook` file
to your repo and tang will run it when pushed, lighting up your
github repos with red, green, and amber lights.

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

SIGQUIT - quits.
SIGINT - restart by reloading the executable.
EOF (on stdin) - quits an interactive tang.

The URLs tang responds to are:

/hook - for handling calls from github.com (checks out repo and
        runs `tang.hook`).

/tang/logs - serve the log directory

/tang - for experimentation and testing

URLs in the domain `qa.scraperwiki.com` are routed to the
products built in the `tang.hook` (not yet):

<branch>.<repo-name>.qa.scraperwiki.com (how does tang know that
there is a docker container to connect to?)

We expect that a foodev.custard.qa.scraperwiki.com will
generally be configured to connect to
foodev.cobalt.qa.scraperwiki.com (really?).


Image by H. Zell used under GFDL:
http://en.wikipedia.org/wiki/File:Acanthurus_leucosternon_01.JPG


Roadmap
=======

- ✓ On git push, update a local clone, if tang.hook exists check it out and invoke tang.hook.
- ✓ (for now) Only run for 'allowed pushers'
- ✓ Tang runs inside a docker container
- tang-event script for synthesizing github events
- Have an interface for "starting" and "stopping"
- Provide a persistent data volume (assume we can trust tang.hook for now, later we can have auth by repository)
- Tang runs tang.hook inside docker containers
