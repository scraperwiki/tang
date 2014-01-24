from scraperwiki/go

workdir /tang

env GOPATH /tang
env PATH /tang/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# TODO(pwaller), later: this is here for one of our packages, but really this
# should be solved using a dockerfile for the package itself.
run apt-get install -yy python-pip
run pip install --upgrade pip
run apt-get install python-lxml

# TODO(pwaller, drj): Remove this when we do docker-inside-docker
run mkdir /var/docker-outside-docker
run ln -s /var/docker-outside-docker/docker.sock /var/run/docker.sock

entrypoint ["./start-tang"]

#entrypoint ["bash", "-c", "mkdir -p /tang/src/github.com/scraperwiki && ln -sT /tang /tang/src/github.com/scraperwiki/tang && env && ./install-tang && exec tang \"$@\""]

expose 8080
