#!/usr/bin/env perl
# OpenUsenet bridge: run cleanfeed-ng filter_art() on one RFC 822 article from stdin.
#
# Install cleanfeed-ng, then point openusenet config at this script:
#   cleanfeed:
#     enabled: true
#     command: perl /usr/local/lib/openusenet/cleanfeed-filter.pl
#     script: /usr/local/news/filter/cleanfeed-ng/cleanfeed
#     config_dir: /etc/news/filter/cleanfeed-ng
#
# Exit 0 = accept; exit 1 with reason on stderr = reject (matches INN filter_art).

use 5.038;
use strict;
use warnings;

	my $cleanfeed = $ENV{CLEANFEED_SCRIPT} || '/usr/local/lib/cleanfeed-ng/cleanfeed';
die "cleanfeed-ng script not readable: $cleanfeed\n" unless -r $cleanfeed;

local $/;
my $raw = <STDIN>;
die "empty article on stdin\n" unless defined $raw && length $raw;

do $cleanfeed or die "load cleanfeed-ng: $@\n";
die "filter_art not defined after loading cleanfeed-ng\n" unless defined &filter_art;

our %hdr;
%hdr = parse_article($raw);

my $verdict = filter_art();
if (defined $verdict && $verdict ne '') {
    print STDERR $verdict, "\n";
    exit 1;
}
exit 0;

sub parse_article {
    my ($raw) = @_;
    my ($head, $body) = split(/\r?\n\r?\n/, $raw, 2);
    $body //= '';
    $body =~ s/\r\n/\n/g;
    $head //= '';

    my %h;
    my $last = '';
    for my $line (split(/\r?\n/, $head)) {
        if ($line =~ /^[ \t]/ && $last ne '') {
            $h{$last} .= ' ' . $line;
            $h{$last} =~ s/^\s+|\s+$//g;
            next;
        }
        if ($line =~ /^([^:\s][^:]*):\s*(.*)/) {
            $last = $1;
            $h{$last} = $2;
            next;
        }
    }

    $h{'__BODY__'} = $body;
    $h{'__LINES__'} = ($body eq '') ? 0 : ($body =~ tr/\n//) + ($body !~ /\n\z/ ? 1 : 0);
    return %h;
}
