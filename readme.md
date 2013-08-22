webfs
=====

a go reimplementation of [webfs](http://man.cat-v.org/9front/4/webfs)

linux
-----

requirements: [plan9port](http://swtch.com/plan9port/)

alternatively, one may use the linux kernel 9p driver

    # fetch
    go get github.com/mischief/webfs

    # run
    webfs &

    # mount
    9 srv -n 'tcp!127.0.0.1!5640' webfs
    mkdir $HOME/webfs
    9pfuse `namespace`/webfs $HOME/webfs

    # usage
    url=https://www.kernel.org/doc/Documentation/filesystems/9p.txt
    cd $HOME/webfs
    d=$(cat clone)
    echo url $url >> $d/ctl
    less $d/body

plan 9
------

    # fetch
    hget https://github.com/mischief/webfs/archive/master.zip > webfs.zip
    unzip webfs.zip
    cd webfs-master
    go build

    # run
    ./webfs &

    # mount
    srv 'tcp!$ip!5640' web /mnt/web

    # usage
    url=https://www.kernel.org/doc/Documentation/filesystems/9p.txt
    cd /mnt/web
    d=`{cat clone}
    echo url $url >> $d/ctl
    cat $d/body

