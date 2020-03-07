---
title: parse_log
type: processor
---

<!--
     THIS FILE IS AUTOGENERATED!

     To make changes please edit the contents of:
     lib/processor/parse_log.go
-->


Parses common log [formats](#formats) into [structured data](#codecs). This is
easier and often much faster than [`grok`](/docs/components/processors/grok).


import Tabs from '@theme/Tabs';

<Tabs defaultValue="common" values={[
  { label: 'Common', value: 'common', },
  { label: 'Advanced', value: 'advanced', },
]}>

import TabItem from '@theme/TabItem';

<TabItem value="common">

```yaml
# Common config fields, showing default values
parse_log:
  format: syslog_rfc5424
  codec: json
```

</TabItem>
<TabItem value="advanced">

```yaml
# All config fields, showing default values
parse_log:
  format: syslog_rfc5424
  codec: json
  best_effort: true
  allow_rfc3339: true
  default_year: current
  default_timezone: UTC
  parts: []
```

</TabItem>
</Tabs>

## Fields

### `format`

A common log [format](#formats) to parse.


Type: `string`  
Default: `"syslog_rfc5424"`  
Options: `syslog_rfc5424`, `syslog_rfc3164`.

### `codec`

Specifies the structured format to parse a log into.


Type: `string`  
Default: `"json"`  
Options: `json`.

### `best_effort`

Still returns partially parsed messages even if an error occurs.


Type: `bool`  
Default: `true`  

### `allow_rfc3339`

Also accept timestamps in rfc3339 format while parsing. Applicable to format `syslog_rfc3164`.


Type: `bool`  
Default: `true`  

### `default_year`

Sets the strategy used to set the year for rfc3164 timestamps. Applicable to format `syslog_rfc3164`. When set to `current` the current year will be set, when set to an integer that value will be used. Leave this field empty to not set a default year at all.


Type: `string`  
Default: `"current"`  

### `default_timezone`

Sets the strategy to decide the timezone for rfc3164 timestamps. Applicable to format `syslog_rfc3164`. This value should follow the [time.LoadLocation](https://golang.org/pkg/time/#LoadLocation) format.


Type: `string`  
Default: `"UTC"`  

### `parts`

An optional array of message indexes of a batch that the processor should apply to.
If left empty all messages are processed. This field is only applicable when
batching messages [at the input level](/docs/configuration/batching).

Indexes can be negative, and if so the part will be selected from the end
counting backwards starting from -1.


Type: `array`  
Default: `[]`  

## Codecs

Currently the only supported structured data codec is `json`.

## Formats

### `syslog_rfc5424`

Attempts to parse a log following the [Syslog rfc5424](https://tools.ietf.org/html/rfc5424)
spec. The resulting structured document may contain any of the following fields:

- `message` (string)
- `timestamp` (string, RFC3339)
- `facility` (int)
- `severity` (int)
- `priority` (int)
- `version` (int)
- `hostname` (string)
- `procid` (string)
- `appname` (string)
- `msgid` (string)
- `structureddata` (object)

### `syslog_rfc3164`

Attempts to parse a log following the [Syslog rfc3164](https://tools.ietf.org/html/rfc3164)
spec. The resulting structured document may contain any of the following fields:

- `message` (string)
- `timestamp` (string, RFC3339)
- `facility` (int)
- `severity` (int)
- `priority` (int)
- `hostname` (string)
- `procid` (string)
- `appname` (string)
- `msgid` (string)

