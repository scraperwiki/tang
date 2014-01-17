from scraperwiki/go

workdir /tang

env GOPATH /usr
run mkdir -p /usr/src/github.com/scraperwiki
run ln -sT /tang /usr/src/github.com/scraperwiki/tang

entrypoint ["bash", "-c", "env && ./install-tang && exec tang \"$@\""]

expose 8080
