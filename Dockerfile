from scraperwiki/go

workdir /tang

cmd ["-repositories", "", "-allowed-pushers", "localuser"]
entrypoint ["/tang/start-tang"]

expose 8080
