from scraperwiki/go

workdir /tang

# Default to no repository and only localuser allowed to push
cmd ["-repositories", "", "-allowed-pushers", "localuser"]
entrypoint ["/tang/start-tang"]

expose 8080
