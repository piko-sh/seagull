# Copyright 2026 PolitePixels Limited
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This project stands against fascism, authoritarianism, and all forms of
# oppression. We built this to empower people, not to enable those who would
# strip others of their rights and dignity.

abs(x double) any
ceil(x double) any
ceiling(x double) any
floor(x double) any
round(x double) any
round(x double, d int) any
truncate(x double, d int) any
sign(x double) int
sqrt(x double) double
power(x double, y double) double
pow(x double, y double) double
exp(x double) double
ln(x double) double
log(x double) double
log(base double, x double) double
log2(x double) double
log10(x double) double
mod(x double, y double) any called-on-null
pi() double never-null
rand() double never-null
rand(seed int) double
degrees(x double) double
radians(x double) double
crc32(expression text) int_unsigned
cot(x double) double
acos(x double) double called-on-null
asin(x double) double called-on-null
atan(x double) double
atan(y double, x double) double
atan2(y double, x double) double
sin(x double) double
cos(x double) double
tan(x double) double

conv(number text, from_base int, to_base int) text called-on-null
bin(number bigint) text
oct(number bigint) text
bit_count(value any) bigint

concat(value any) text variadic=1
concat_ws(separator text, value any) text nulling-argument=0 variadic=2

length(string text) int
char_length(string text) int
character_length(string text) int
octet_length(string text) int

lower(string text) text
lcase(string text) text
upper(string text) text
ucase(string text) text

trim(string text) text
ltrim(string text) text
rtrim(string text) text

lpad(string text, length int, pad text) text
lpad(string text, length int) text
rpad(string text, length int, pad text) text
rpad(string text, length int) text

repeat(string text, count int) text
replace(string text, from_str text, to_str text) text
reverse(string text) text
substring(string text, start int) text
substring(string text, start int, length int) text
substr(string text, start int) text
substr(string text, start int, length int) text
mid(string text, start int, length int) text
left(string text, length int) text
right(string text, length int) text

locate(substring text, string text) int
locate(substring text, string text, start int) int
position(substring text, string text) int
instr(string text, substring text) int
find_in_set(string text, string_list text) int

ascii(string text) int
hex(x any) text
unhex(string text) blob
char(code int) blob never-null variadic=1
weight_string(string text) blob
space(n int) text
format(x double, d int) text
format(x double, d int, locale text) text
bit_length(string text) int
ord(string text) bigint
strcmp(expr1 text, expr2 text) int
soundex(string text) text
quote(string text) text
insert(string text, position int, length int, new_string text) text
substring_index(string text, delimiter text, count int) text
export_set(bits bigint, on_string text, off_string text, separator text?, number_of_bits int?) text

field(value any, candidate any) int never-null variadic=2
elt(index int, string text) text called-on-null variadic=2
make_set(bits bigint, string text) text nulling-argument=0 variadic=2

regexp_like(expression text, pattern text, match_type text?) boolean
regexp_instr(expression text, pattern text, position int?, occurrence int?, return_option int?, match_type text?) int
regexp_substr(expression text, pattern text, position int?, occurrence int?, match_type text?) text called-on-null
regexp_replace(expression text, pattern text, replacement text, position int?, occurrence int?, match_type text?) text

md5(string text) text
sha1(string text) text
sha(string text) text
sha2(string text, hash_length int) text called-on-null
to_base64(string text) text
from_base64(string text) varbinary called-on-null
aes_encrypt(string text, key text, init_vector text?) varbinary
aes_decrypt(crypt varbinary, key text, init_vector text?) varbinary called-on-null
compress(string text) varbinary
uncompress(string varbinary) blob called-on-null
uncompressed_length(string varbinary) int
random_bytes(length int) varbinary

now() datetime never-null
now(precision int) datetime never-null
sysdate() datetime never-null
sysdate(precision int) datetime never-null
curdate() date never-null
current_date() date never-null
curtime() time never-null
curtime(precision int) time never-null
current_time() time never-null
current_timestamp() datetime never-null
localtime() datetime never-null
localtimestamp() datetime never-null
utc_date() date never-null
utc_time() time never-null
utc_timestamp() datetime never-null

date(expression datetime) date
time(expression datetime) time
year(date date) int
month(date date) int
day(date date) int
dayofmonth(date date) int
hour(time time) int
minute(time time) int
second(time time) int
dayofweek(date date) int
dayofyear(date date) int
week(date date) int
week(date date, mode int) int
quarter(date date) int
last_day(date date) date
monthname(date date) text
dayname(date date) text
weekday(date date) int
yearweek(date date) int
yearweek(date date, mode int) int
weekofyear(date date) int
microsecond(time time) int
to_days(date date) int
to_seconds(date datetime) bigint
from_days(days int) date
time_to_sec(time time) bigint
sec_to_time(seconds bigint) time
timestamp(expression datetime) datetime
timestamp(expression datetime, time any) datetime

extract(value any) any

makedate(year int, day_of_year int) date called-on-null
maketime(hour int, minute int, second double) time called-on-null

date_format(date datetime, format text) text
time_format(time time, format text) text
get_format(kind text, standard text) text
str_to_date(string text, format text) datetime called-on-null
from_unixtime(timestamp bigint) datetime
from_unixtime(timestamp bigint, format text) text
unix_timestamp() bigint called-on-null

unix_timestamp(date datetime) any

datediff(expr1 date, expr2 date) int
timediff(expr1 time, expr2 time) time
timestampdiff(unit text, expr1 datetime, expr2 datetime) bigint
timestampadd(unit text, interval int, datetime datetime) any
date_add(date datetime, interval any) any
date_sub(date datetime, interval any) any
adddate(date datetime, days any) any
subdate(date datetime, days any) any
addtime(date datetime, time any) any
subtime(date datetime, time any) any
period_add(period int, months int) bigint
period_diff(period1 int, period2 int) bigint

convert_tz(date datetime, from_zone text, to_zone text) datetime called-on-null

json_extract(json json, path text) json called-on-null variadic=2
json_unquote(json json) text
json_value(json json, path text) text called-on-null
json_quote(string text) text
json_search(json json, one_or_all text, search_str text, value any) json called-on-null variadic=3

json_set(json json, path text, value any) json variadic=3
json_insert(json json, path text, value any) json variadic=3
json_replace(json json, path text, value any) json variadic=3
json_array_append(json json, path text, value any) json variadic=3
json_array_insert(json json, path text, value any) json variadic=3
json_remove(json json, path text) json variadic=2
json_merge_patch(json json, value json) json variadic=2
json_merge_preserve(json json, value json) json variadic=2
json_merge(json json, value json) json variadic=2

json_object(key text, value any) json never-null variadic=0
json_array(value any) json never-null variadic=0

json_contains(target json, candidate json) boolean
json_contains(target json, candidate json, path text) boolean called-on-null
json_contains_path(json json, one_or_all text, path text) boolean variadic=3
json_overlaps(json json, candidate json) boolean
json_type(json json) text
json_valid(value any) boolean
json_length(json json) int
json_length(json json, path text) int called-on-null
json_depth(json json) int
json_storage_size(json json) int
json_pretty(json json) text
json_keys(json json) json called-on-null
json_keys(json json, path text) json called-on-null
json_schema_valid(schema json, json json) boolean
json_schema_validation_report(schema json, json json) json

json_arrayagg(value any) json called-on-null aggregate
json_objectagg(key text, value any) json called-on-null aggregate

count() bigint never-null aggregate
count(x any) bigint never-null aggregate variadic=1
sum(x any) decimal called-on-null aggregate
avg(x any) double called-on-null aggregate
min(x any) any called-on-null aggregate
max(x any) any called-on-null aggregate
group_concat(expression any) text called-on-null aggregate
bit_and(x any) any called-on-null aggregate
bit_or(x any) any called-on-null aggregate
bit_xor(x any) any called-on-null aggregate
std(x any) double called-on-null aggregate
stddev(x any) double called-on-null aggregate
stddev_pop(x any) double called-on-null aggregate
stddev_samp(x any) double called-on-null aggregate
variance(x any) double called-on-null aggregate
var_pop(x any) double called-on-null aggregate
var_samp(x any) double called-on-null aggregate

any_value(x any) any return-type-from=1
grouping(expression any) int never-null variadic=1

row_number() bigint never-null
rank() bigint never-null
dense_rank() bigint never-null
ntile(n int) int never-null
lag(expression any, offset int, default any) any called-on-null min-arguments=1
lead(expression any, offset int, default any) any called-on-null min-arguments=1
first_value(expression any) any called-on-null
last_value(expression any) any called-on-null
nth_value(expression any, n int) any called-on-null
percent_rank() double never-null
cume_dist() double never-null

if(condition boolean, then any, else any) any called-on-null
ifnull(expr1 any, expr2 any) any called-on-null
nullif(expr1 any, expr2 any) any called-on-null
coalesce(value any) any called-on-null variadic=1
greatest(value any) any variadic=1
least(value any) any variadic=1

version() text never-null
database() text never-null
user() text never-null
current_user() text never-null
last_insert_id() bigint called-on-null
last_insert_id(value bigint) bigint_unsigned
uuid() text never-null
uuid_short() bigint_unsigned never-null
uuid_to_bin(uuid text, swap_flag any?) varbinary nulling-argument=0
bin_to_uuid(binary varbinary, swap_flag any?) text nulling-argument=0
is_uuid(string text) boolean
connection_id() bigint never-null
session_user() text never-null
system_user() text never-null
schema() text called-on-null
row_count() bigint never-null
found_rows() bigint never-null
charset(string any) text never-null
collation(string any) text never-null
coercibility(string any) int never-null
isnull(expression any) boolean never-null
sleep(duration double) int never-null
benchmark(count bigint, expression any) int called-on-null
format_bytes(count double) text
format_pico_time(time double) text

get_lock(name text, timeout double) int called-on-null
release_lock(name text) int called-on-null
is_free_lock(name text) int called-on-null
is_used_lock(name text) bigint called-on-null
release_all_locks() bigint never-null

inet_aton(expression text) bigint
inet_ntoa(expression bigint) text
inet6_aton(address varchar) varbinary
inet6_ntoa(value varbinary) varchar
is_ipv4(expression text) bigint
is_ipv6(expression text) bigint
is_ipv4_mapped(expression varbinary) bigint
is_ipv4_compat(expression varbinary) bigint
