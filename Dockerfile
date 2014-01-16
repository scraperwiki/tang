from scraperwiki/go

workdir /tang

env GOPATH /usr
run mkdir -p /usr/src/github.com/scraperwiki
run ln -sT /tang /usr/src/github.com/scraperwiki/tang

cmd ["bash", "-c", "./install-tang && exec tang"]

expose 8080
