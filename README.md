Tang
====


![Ancanthurus leucosternon](http://upload.wikimedia.org/wikipedia/commons/thumb/3/36/Acanthurus_leucosternon_01.JPG/640px-Acanthurus_leucosternon_01.JPG "Powder Blue Tang")

Tang installs itself as a github service hook, then listens. It
does stuff when you push to `github.com`.

    go build

    # Set GITHUB_USER and GITHUB_PASSWORD
    . ./github-password.sh
    sudo -E ./tang

Image by H. Zell used under GFDL:
http://en.wikipedia.org/wiki/File:Acanthurus_leucosternon_01.JPG


Roadmap
=======

- ✓ On git push, update a local clone, if tang.hook exists check it out and invoke tang.hook.
- ✓ (for now) Only run for 'allowed users'
- ✓ Tang runs inside a docker container
- Provide a persistent data volume (assume we can trust)