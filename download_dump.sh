#!/bin/bash
# Common utils
wget_cmd="wget -q --show-progress --limit-rate=10M"
out_dir=$(dirname -- "$0")/dump

$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwiki-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwiki-pages-articles-multistream-index.txt.bz2

