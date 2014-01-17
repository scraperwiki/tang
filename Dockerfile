from scraperwiki/go

workdir /tang

env GOPATH /tang
env PATH /tang/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

entrypoint ["./start-tang"]

#entrypoint ["bash", "-c", "mkdir -p /tang/src/github.com/scraperwiki && ln -sT /tang /tang/src/github.com/scraperwiki/tang && env && ./install-tang && exec tang \"$@\""]

expose 8080
