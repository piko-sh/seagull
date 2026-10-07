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

count() UInt64 aggregate
count(any) UInt64 aggregate
sum(any) any aggregate
avg(any) Float64 aggregate
min(any) any aggregate
max(any) any aggregate
any(any) any aggregate
anyLast(any) any aggregate
anyHeavy(any) any aggregate

uniq(any) UInt64 aggregate
uniqExact(any) UInt64 aggregate
uniqHLL12(any) UInt64 aggregate
uniqCombined(any) UInt64 aggregate
uniqCombined64(any) UInt64 aggregate
uniqTheta(any) UInt64 aggregate

groupArray(any) any aggregate
groupUniqArray(any) any aggregate
groupArrayMovingSum(Float64) Array(Float64) aggregate
groupArrayMovingAvg(Float64) Array(Float64) aggregate
groupBitOr(any) any aggregate
groupBitAnd(any) any aggregate
groupBitXor(any) any aggregate
groupArrayLast(UInt64, any) any aggregate
groupArrayInsertAt(any, UInt64) any aggregate
groupArraySample(UInt64, any) any aggregate
groupArraySorted(UInt64, any) any aggregate
groupConcat(String) String aggregate
groupConcat(String, String) String aggregate

grouping(any) UInt64 never-null variadic=1

argMin(any, any) any aggregate
argMax(any, any) any aggregate
quantile(Float64) Float64 aggregate
quantileExact(any) any aggregate
quantileTDigest(Float64) Float32 aggregate
median(Float64) Float64 aggregate
quantiles(Float64) Array(Float64) aggregate

varPop(Float64) Float64 aggregate
varSamp(Float64) Float64 aggregate
stddevPop(Float64) Float64 aggregate
stddevSamp(Float64) Float64 aggregate
covarPop(Float64, Float64) Float64 aggregate
covarSamp(Float64, Float64) Float64 aggregate
corr(Float64, Float64) Float64 aggregate

topK(any) any aggregate
topKWeighted(any, UInt64) any aggregate
simpleLinearRegression(Float64, Float64) Array(Float64) aggregate
stochasticLinearRegression(Float64, Float64) Array(Float64) aggregate
approx_top_k(any) Array(any) aggregate
approx_top_sum(any, UInt64) Array(any) aggregate

avgWeighted(Float64, Float64) Float64 aggregate
sumWithOverflow(any) any aggregate
sumKahan(Float64) Float64 aggregate
sumCount(any) any aggregate

quantileTiming(Float64) Float32 aggregate
quantileTimingWeighted(Float64, UInt64) Float32 aggregate
quantileExactWeighted(any, UInt64) any aggregate
quantileInterpolatedWeighted(Float64, UInt64) Float64 aggregate
quantileBFloat16(Float64) Float64 aggregate
quantileGK(Float64) Float64 aggregate
quantileDeterministic(Float64, UInt64) Float64 aggregate

kurtPop(Float64) Float64 aggregate
kurtSamp(Float64) Float64 aggregate
skewPop(Float64) Float64 aggregate
skewSamp(Float64) Float64 aggregate

exponentialMovingAverage(Float64, UInt64) Float64 aggregate
exponentialTimeDecayedAvg(Float64, Float64, UInt64) Float64 aggregate
exponentialTimeDecayedCount(Float64, UInt64) Float64 aggregate
exponentialTimeDecayedMax(Float64, Float64, UInt64) Float64 aggregate
exponentialTimeDecayedSum(Float64, Float64, UInt64) Float64 aggregate

largestTriangleThreeBuckets(UInt64, Float64, Float64) Array(any) aggregate
boundingRatio(Float64, Float64) Float64 aggregate
histogram(UInt64, Float64) Array(Tuple(lower Float64, upper Float64, height Float64)) aggregate
singleValueOrNull(any) any aggregate
entropy(any) Float64 aggregate

intervalLengthSum(DateTime, DateTime) Float64 aggregate
deltaSum(Float64) Float64 aggregate
deltaSumTimestamp(Float64, DateTime) Float64 aggregate

now() DateTime
now64() DateTime64
now64(UInt64) DateTime64
today() Date
yesterday() Date
toDate(String) Date
toDate(DateTime) Date
toDateTime(String) DateTime
toDateTime(UInt64) DateTime
toDateTime(DateTime) DateTime
toDateTime(String, String) DateTime
toDateTime(UInt64, String) DateTime
toDateTime(DateTime, String) DateTime
toDateTime64(String, UInt64) DateTime64
toYear(Date) UInt16
toMonth(Date) UInt8
toDayOfMonth(Date) UInt8
toDayOfWeek(Date) UInt8
toHour(DateTime) UInt8
toMinute(DateTime) UInt8
toSecond(DateTime) UInt8
toUnixTimestamp(DateTime) UInt32
toUnixTimestamp64Milli(DateTime64) Int64
toStartOfYear(Date) Date
toStartOfMonth(Date) Date
toStartOfWeek(Date) Date
toStartOfDay(DateTime) DateTime
toStartOfHour(DateTime) DateTime
toStartOfMinute(DateTime) DateTime
toStartOfFiveMinute(DateTime) DateTime
toStartOfFifteenMinutes(DateTime) DateTime
formatDateTime(DateTime, String) String
parseDateTime(String, String) DateTime
parseDateTimeBestEffort(String) DateTime
parseDateTime64BestEffort(String, UInt64) DateTime64
dateDiff(String, DateTime, DateTime) Int64

dateAdd(any, Int64, any) any
date_add(any, Int64, any) any
timestampAdd(any, Int64, any) any
timestamp_add(any, Int64, any) any
dateSub(any, Int64, any) any
date_sub(any, Int64, any) any
timestampSub(any, Int64, any) any
timestamp_sub(any, Int64, any) any
addDays(any, Int64) any
addHours(any, Int64) any
addMinutes(any, Int64) any
addSeconds(any, Int64) any
subtractDays(any, Int64) any
subtractHours(any, Int64) any
subtractMinutes(any, Int64) any
toTimeZone(DateTime, String) DateTime
timezone() String

dateName(String, DateTime) String
age(String, DateTime, DateTime) Int64
monthName(Date) String
formatDateTimeInJodaSyntax(DateTime, String) String
formatDateTimeInJodaSyntax(DateTime, String, String) String
parseDateTimeBestEffortOrNull(String) DateTime
parseDateTimeBestEffortOrZero(String) DateTime
parseDateTimeInJodaSyntax(String, String) DateTime
parseDateTimeInJodaSyntaxOrNull(String, String) DateTime
parseDateTimeInJodaSyntaxOrZero(String, String) DateTime
parseDateTime32BestEffort(String) DateTime
parseDateTime32BestEffortOrNull(String) DateTime
parseDateTime32BestEffortOrZero(String) DateTime
parseDateTime64BestEffortOrNull(String, UInt64) DateTime64
parseDateTime64BestEffortOrZero(String, UInt64) DateTime64

toStartOfQuarter(Date) Date
toStartOfTenMinutes(DateTime) DateTime
toStartOfInterval(DateTime, String) DateTime
toStartOfInterval(DateTime, IntervalSecond) any
toStartOfInterval(DateTime, IntervalSecond, String) any
toQuarter(Date) UInt8
toWeek(Date) UInt8
toWeek(Date, UInt64) UInt8
toISOWeek(Date) UInt8
toISOYear(Date) UInt16
toYearWeek(Date) UInt64
toYearWeek(Date, UInt64) UInt64
toDayOfYear(Date) UInt16

toYYYYMM(Date) UInt32
toYYYYMMDD(Date) UInt32
toYYYYMMDDhhmmss(DateTime) UInt64
toMonday(Date) Date
toLastDayOfMonth(Date) Date
toLastDayOfWeek(Date) Date
toLastDayOfWeek(Date, UInt8) Date
toStartOfISOYear(Date) Date
toStartOfFiveMinutes(DateTime) DateTime
toStartOfSecond(DateTime64) any return-type-from=1
toStartOfMillisecond(DateTime64) any return-type-from=1
toStartOfMicrosecond(DateTime64) DateTime64(6)
toStartOfNanosecond(DateTime64) DateTime64(9)
toMillisecond(DateTime) UInt16
toDaysSinceYearZero(Date) UInt32
fromDaysSinceYearZero(Int64) Date
YYYYMMDDToDate(UInt32) Date
YYYYMMDDToDate32(UInt32) Date32
YYYYMMDDhhmmssToDateTime(UInt64) DateTime
YYYYMMDDhhmmssToDateTime64(UInt64) DateTime64(3)
toUnixTimestamp64Second(DateTime64) Int64
toDate32OrZero(String) Date32
timezoneOf(DateTime) String
timezoneOffset(DateTime) Int32
timeSlot(DateTime) DateTime
timeSlots(DateTime, UInt32) Array(DateTime)
timeSlots(DateTime, UInt32, UInt32) Array(DateTime)
timestamp(String) DateTime64(6)
timestamp(String, String) DateTime64(6)
toTime(DateTime) DateTime
toTime(DateTime, String) DateTime
addDate(any, any) any return-type-from=1
subDate(any, any) any return-type-from=1

toIntervalNanosecond(Int64) IntervalNanosecond
toIntervalMicrosecond(Int64) IntervalMicrosecond
toIntervalMillisecond(Int64) IntervalMillisecond
toIntervalSecond(Int64) IntervalSecond
toIntervalMinute(Int64) IntervalMinute
toIntervalHour(Int64) IntervalHour
toIntervalDay(Int64) IntervalDay
toIntervalWeek(Int64) IntervalWeek
toIntervalMonth(Int64) IntervalMonth
toIntervalQuarter(Int64) IntervalQuarter
toIntervalYear(Int64) IntervalYear

fromUnixTimestamp(Int64) DateTime
fromUnixTimestamp(Int64, String) String
fromUnixTimestamp(Int64, String, String) String
fromUnixTimestampInJodaSyntax(Int64, String) String
fromUnixTimestampInJodaSyntax(Int64, String, String) String
parseDateTimeBestEffortUS(String) DateTime
parseDateTime64BestEffortUS(String) DateTime64(3)
parseDateTime64BestEffortUSOrNull(String) DateTime64(3)
parseDateTime64BestEffortUSOrZero(String) DateTime64(3)
formatReadableSize(Float64) String
formatReadableDecimalSize(Float64) String
formatReadableQuantity(Float64) String
formatReadableTimeDelta(Float64) String
formatReadableTimeDelta(Float64, String) String

addMonths(any, Int64) any
addQuarters(any, Int64) any
addWeeks(any, Int64) any
addYears(any, Int64) any
addMilliseconds(any, Int64) any
addMicroseconds(any, Int64) any
addNanoseconds(any, Int64) any
subtractMonths(any, Int64) any
subtractQuarters(any, Int64) any
subtractWeeks(any, Int64) any
subtractYears(any, Int64) any
subtractSeconds(any, Int64) any
subtractMilliseconds(any, Int64) any
subtractMicroseconds(any, Int64) any
subtractNanoseconds(any, Int64) any
changeYear(DateTime, Int64) DateTime
changeMonth(DateTime, Int64) DateTime
changeDay(DateTime, Int64) DateTime
changeHour(DateTime, Int64) DateTime
changeMinute(DateTime, Int64) DateTime
changeSecond(DateTime, Int64) DateTime

dateTrunc(String, DateTime) DateTime
dateTrunc(String, DateTime, String) DateTime
makeDate(UInt64, UInt64, UInt64) Date
makeDate32(UInt64, UInt64, UInt64) Date
makeDateTime(UInt64, UInt64, UInt64, UInt64, UInt64, UInt64) DateTime
makeDateTime(UInt64, UInt64, UInt64, UInt64, UInt64, UInt64, String) DateTime
makeDateTime64(UInt64, UInt64, UInt64, UInt64, UInt64, UInt64, UInt64) DateTime64
toDate32(any) Date
fromUnixTimestamp64Milli(Int64) DateTime64
fromUnixTimestamp64Micro(Int64) DateTime64
fromUnixTimestamp64Nano(Int64) DateTime64
toUnixTimestamp64Micro(DateTime64) Int64
toUnixTimestamp64Nano(DateTime64) Int64

length(String) UInt64
empty(String) UInt8
notEmpty(String) UInt8
length(Array(any)) UInt64
empty(Array(any)) UInt8
notEmpty(Array(any)) UInt8
length(Map(any, any)) UInt64
empty(Map(any, any)) UInt8
notEmpty(Map(any, any)) UInt8
lower(String) String
upper(String) String
lowerUTF8(String) String
upperUTF8(String) String
reverse(String) String
concat(String) String variadic=1
concatWithSeparator(String, String) String variadic=2
substring(String, UInt64, UInt64) String
substring(String, UInt64) String
substringUTF8(String, UInt64, UInt64) String
position(String, String) UInt64
positionUTF8(String, String) UInt64
replaceAll(String, String, String) String
replaceOne(String, String, String) String
replaceRegexpAll(String, String, String) String
replaceRegexpOne(String, String, String) String
splitByChar(String, String) Array(String)
splitByString(String, String) Array(String)
splitByWhitespace(String) Array(String)
splitByRegexp(String, String) Array(String)
splitByRegexp(String, String, UInt64) Array(String)
splitByNonAlpha(String) Array(String)
splitByNonAlpha(String, UInt64) Array(String)
alphaTokens(String) Array(String)
alphaTokens(String, UInt64) Array(String)
ngrams(String, UInt64) Array(String)
tokens(String) Array(String)
tokens(String, String) Array(String)
arrayStringConcat(Array(String)) String
arrayStringConcat(Array(String), String) String
format(String, any) String
printf(String) String
trimBoth(String) String
trimLeft(String) String
trimRight(String) String
like(String, String) UInt8
notLike(String, String) UInt8
ilike(String, String) UInt8
match(String, String) UInt8
extract(String, String) String
extractAll(String, String) Array(String)
startsWith(String, String) UInt8
endsWith(String, String) UInt8

lengthUTF8(String) UInt64
leftUTF8(String, Int64) String
rightUTF8(String, Int64) String
left(String, Int64) String
right(String, Int64) String
leftPad(String, UInt64) String
leftPad(String, UInt64, String) String
rightPad(String, UInt64) String
rightPad(String, UInt64, String) String
leftPadUTF8(String, UInt64) String
leftPadUTF8(String, UInt64, String) String
rightPadUTF8(String, UInt64) String
rightPadUTF8(String, UInt64, String) String
repeat(String, UInt64) String
space(UInt64) String
reverseUTF8(String) String

regexpExtract(String, String) String
regexpExtract(String, String, UInt64) String
regexpQuoteMeta(String) String
initcap(String) String
initcapUTF8(String) String
ascii(String) Int64
isValidUTF8(String) UInt8
notILike(String, String) UInt8

hasToken(String, String) UInt8
hasTokenCaseInsensitive(String, String) UInt8
hasTokenOrNull(String, String) UInt8
multiSearchAny(String, Array(String)) UInt8
multiSearchAnyCaseInsensitive(String, Array(String)) UInt8
multiMatchAny(String, Array(String)) UInt8
multiMatchAllIndices(String, Array(String)) Array(UInt64)
countSubstrings(String, String) UInt64
countMatches(String, String) UInt64
positionCaseInsensitive(String, String) UInt64
positionCaseInsensitiveUTF8(String, String) UInt64
locate(String, String) UInt64

arrayMap(any, Array(any)) any
arrayFilter(any, Array(any)) any
arrayReduce(String, Array(any)) any
arrayFold(any, Array(any), any) any
arrayEnumerate(Array(any)) Array(UInt64)
arrayDistinct(Array(any)) any
arrayConcat(Array(any)) any variadic=1
arraySlice(Array(any), Int64, Int64) any
arraySlice(Array(any), Int64) any
arrayPushBack(Array(any), any) any
arrayPushFront(Array(any), any) any
arrayPopBack(Array(any)) any
arrayPopFront(Array(any)) any
arrayResize(Array(any), Int64) any
arrayElement(Array(any), Int64) any
has(Array(any), any) UInt8
hasAll(Array(any), Array(any)) UInt8
hasAny(Array(any), Array(any)) UInt8
hasSubstr(Array(any), Array(any)) UInt8
indexOf(Array(any), any) UInt64
countEqual(Array(any), any) UInt64
arraySort(Array(any)) any
arrayReverseSort(Array(any)) any
arrayUniq(Array(any)) UInt64
arrayDifference(Array(Float64)) Array(Float64)
arrayCumSum(Array(any)) any
arrayCompact(Array(any)) any
array(any) any variadic=0
range(UInt64) Array(UInt64)
range(UInt64, UInt64) Array(UInt64)

tuple(any) any variadic=1
tupleElement(any, Int64) any
untuple(any) any
map(any) any variadic=0
mapKeys(any) any
mapValues(any) any
mapContains(any, any) UInt8
mapAdd(any, any) any
mapSubtract(any, any) any
mapFilter(any, any) any
mapApply(any, any) any

CAST(any, String) any
accurateCast(any, String) any
accurateCastOrNull(any, String) any
accurateCastOrDefault(any, any) any
toInt8(any) Int64
toInt16(any) Int64
toInt32(any) Int64
toInt64(any) Int64
toInt128(any) Int64
toInt256(any) Int64
toUInt8(any) UInt64
toUInt16(any) UInt64
toUInt32(any) UInt64
toUInt64(any) UInt64
toUInt128(any) UInt64
toUInt256(any) UInt64
toFloat32(any) Float32
toFloat64(any) Float64
toDecimal128(any, Int64) Decimal128
toString(any) String
toFixedString(String, UInt64) String
toUUID(String) UUID
toIPv4(String) IPv4
toIPv6(String) IPv6
toBool(any) Bool

toInt8OrNull(any) Int64
toInt8OrZero(any) Int64
toInt8OrDefault(any) Int64
toInt16OrNull(any) Int64
toInt16OrZero(any) Int64
toInt16OrDefault(any) Int64
toInt32OrNull(any) Int64
toInt32OrZero(any) Int64
toInt32OrDefault(any) Int64
toInt64OrNull(any) Int64
toInt64OrZero(any) Int64
toInt64OrDefault(any) Int64
toInt128OrNull(any) Int64
toInt128OrZero(any) Int64
toInt128OrDefault(any) Int64
toInt256OrNull(any) Int64
toInt256OrZero(any) Int64
toInt256OrDefault(any) Int64
toUInt8OrNull(any) UInt64
toUInt8OrZero(any) UInt64
toUInt8OrDefault(any) UInt64
toUInt16OrNull(any) UInt64
toUInt16OrZero(any) UInt64
toUInt16OrDefault(any) UInt64
toUInt32OrNull(any) UInt64
toUInt32OrZero(any) UInt64
toUInt32OrDefault(any) UInt64
toUInt64OrNull(any) UInt64
toUInt64OrZero(any) UInt64
toUInt64OrDefault(any) UInt64
toUInt128OrNull(any) UInt64
toUInt128OrZero(any) UInt64
toUInt128OrDefault(any) UInt64
toUInt256OrNull(any) UInt64
toUInt256OrZero(any) UInt64
toUInt256OrDefault(any) UInt64

toFloat32OrNull(any) Float32
toFloat64OrNull(any) Float64
toFloat32OrZero(any) Float32
toFloat64OrZero(any) Float64
toFloat32OrDefault(any) Float32
toFloat64OrDefault(any) Float64
toDateOrNull(any) Date
toDateTimeOrNull(any) DateTime
toDateTime64OrNull(any, UInt64) DateTime64
toDateOrZero(any) Date
toDateTimeOrZero(any) DateTime
toDateTime64OrZero(any, UInt64) DateTime64
toDateOrDefault(any) Date
toDateTimeOrDefault(any) DateTime
toDateTime64OrDefault(any, UInt64) DateTime64
parseDateTimeBestEffortUSOrNull(String) DateTime
parseDateTimeBestEffortUSOrZero(String) DateTime

toDecimal32(any, Int64) Decimal32
toDecimal32OrNull(any, Int64) Decimal32
toDecimal32OrZero(any, Int64) Decimal32
toDecimal32OrDefault(any, Int64) Decimal32
toDecimal64(any, Int64) Decimal64
toDecimal64OrNull(any, Int64) Decimal64
toDecimal64OrZero(any, Int64) Decimal64
toDecimal64OrDefault(any, Int64) Decimal64
toDecimal128OrNull(any, Int64) Decimal128
toDecimal128OrZero(any, Int64) Decimal128
toDecimal128OrDefault(any, Int64) Decimal128
toDecimal256(any, Int64) Decimal256
toDecimal256OrNull(any, Int64) Decimal256
toDecimal256OrZero(any, Int64) Decimal256
toDecimal256OrDefault(any, Int64) Decimal256

abs(any) any
round(Float64) any
round(Float64, Int64) any
roundBankers(Float64) any
roundBankers(Float64, Int64) any
floor(Float64) any
floor(Float64, Int64) any
ceil(Float64) any
ceil(Float64, Int64) any
truncate(Float64) any
truncate(Float64, Int64) any
pow(Float64, Float64) Float64
sqrt(Float64) Float64
exp(any) any
log(any) any
log10(Float64) Float64
log2(Float64) Float64
sin(Float64) Float64
cos(Float64) Float64
tan(Float64) Float64
asin(Float64) Float64
acos(Float64) Float64
atan(Float64) Float64
atan2(Float64, Float64) Float64
greatest(any, any) any
least(any, any) any
plus(any, any) any
minus(any, any) any
multiply(any, any) any
divide(any, any) any
intDiv(any, any) any
intDivOrZero(any, any) any
modulo(any, any) any
moduloOrZero(any, any) any
positiveModulo(any, any) any
negate(any) any
pi() Float64
e() Float64
clamp(any, any, any) any
sign(Float64) Int8
factorial(UInt64) UInt64
cbrt(Float64) Float64
widthBucket(Float64, Float64, Float64, UInt64) any

and(any) any variadic=2
or(any) any variadic=2
not(any) any
xor(any) any variadic=2

MD5(String) FixedString(16)
SHA1(String) FixedString(20)
SHA224(String) FixedString(28)
SHA256(String) FixedString(32)
SHA384(String) FixedString(48)
SHA512(String) FixedString(64)
cityHash64(any) UInt64
farmHash64(any) UInt64
intHash64(any) UInt64
xxHash64(any) UInt64
murmurHash2_64(any) UInt64
murmurHash3_64(any) UInt64
xxHash32(any) UInt64
murmurHash3_32(any) UInt64
halfMD5(any) UInt64
sipHash64(any) UInt64

protocol(String) String
domain(String) String
domainWithoutWWW(String) String
topLevelDomain(String) String
path(String) String
pathFull(String) String
queryString(String) String
fragment(String) String
extractURLParameter(String, String) String
IPv4ToString(IPv4) String
IPv6ToString(IPv6) String
IPv4StringToNum(String) UInt32
IPv6StringToNum(String) String
IPv4NumToString(UInt64) String

JSONExtract(String, String, String) any
JSONExtractString(String, String) String
JSONExtractInt(String, String) Int64
JSONExtractUInt(String, String) UInt64
JSONExtractFloat(String, String) Float64
JSONExtractBool(String, String) UInt8
JSONExtractRaw(String, String) String
JSONExtractArrayRaw(String, String) Array(String)
JSONExtractKeys(String, String) Array(String)
JSONHas(String, String) UInt8
JSONKey(String, UInt64) String
JSONLength(String, String) UInt64
JSONType(String, String) String
isValidJSON(String) UInt8
simpleJSONExtractString(String, String) String
simpleJSONExtractInt(String, String) Int64
simpleJSONExtractUInt(String, String) UInt64
simpleJSONExtractFloat(String, String) Float64
simpleJSONExtractBool(String, String) UInt8
simpleJSONExtractRaw(String, String) String
simpleJSONHas(String, String) UInt8
JSON_VALUE(String, String) String
JSON_QUERY(String, String) String
JSON_EXISTS(String, String) UInt8

if(Bool, any, any) any
multiIf(Bool) any variadic=3
nullIf(any, any) any
assumeNotNull(any) any
isNull(any) UInt8
isNotNull(any) UInt8
coalesce(any) any variadic=1
ifNull(any, any) any
toTypeName(any) String

transform(any, Array(any), Array(any)) any
transform(any, Array(any), Array(any), any) any
throwIf(any) UInt8
throwIf(any, String) UInt8
materialize(any) any return-type-from=1
toLowCardinality(any) any return-type-from=1
ignore(any) UInt8 never-null variadic=0
sleep(Float64) UInt8
max2(Float64, Float64) Float64
min2(Float64, Float64) Float64
countDigits(any) UInt8

version() String
hostName() String
currentDatabase() String
currentUser() String
user() String
uptime() UInt64
generateUUIDv4() UUID
generateUUIDv7() UUID
generateUUIDv7(any) UUID
generateSnowflakeID() UInt64
generateSnowflakeID(UInt64) UInt64
snowflakeIDToDateTime(UInt64) DateTime
snowflakeIDToDateTime64(UInt64) DateTime64

row_number() UInt64
rank() UInt64
dense_rank() UInt64
denseRank() UInt64
percent_rank() Float64
percentRank() Float64
cume_dist() Float64
ntile(UInt64) UInt64
lag(any) any
lag(any, UInt64) any
lag(any, UInt64, any) any
lead(any) any
lead(any, UInt64) any
lead(any, UInt64, any) any
lagInFrame(any) any
lagInFrame(any, UInt64) any
lagInFrame(any, UInt64, any) any
leadInFrame(any) any
leadInFrame(any, UInt64) any
leadInFrame(any, UInt64, any) any
first_value(any) any
last_value(any) any
nth_value(any, UInt64) any

bitAnd(Int64, Int64) Int64
bitOr(Int64, Int64) Int64
bitXor(Int64, Int64) Int64
bitShiftLeft(Int64, Int64) Int64
bitShiftRight(Int64, Int64) Int64
bitRotateLeft(Int64, Int64) Int64
bitRotateRight(Int64, Int64) Int64
bitNot(Int64) Int64
bitTest(Int64, Int64) UInt8
bitTestAll(Int64, Int64) UInt8
bitTestAny(Int64, Int64) UInt8
bitCount(Int64) UInt64
bitPositions(Int64) Array(UInt64)

equals(any, any) UInt8
notEquals(any, any) UInt8
less(any, any) UInt8
greater(any, any) UInt8
lessOrEquals(any, any) UInt8
greaterOrEquals(any, any) UInt8
isFinite(Float64) UInt8
isInfinite(Float64) UInt8
isNaN(Float64) UInt8
isZeroOrNull(any) UInt8

rand() UInt64
rand32() UInt64
rand64() UInt64
randConstant() UInt64
randCanonical() Float64
randUniform(Float64, Float64) Float64
randNormal(Float64, Float64) Float64
randLogNormal(Float64, Float64) Float64
randExponential(Float64) Float64
randChiSquared(Float64) Float64
randStudentT(Float64) Float64
randFisherF(Float64, Float64) Float64
randPoisson(Float64) UInt64
randBinomial(UInt64, Float64) UInt64
randBernoulli(Float64) UInt64

encrypt(String, String, String) String
decrypt(String, String, String) String
aes_encrypt_mysql(String, String, String) String
aes_decrypt_mysql(String, String, String) String
base64Encode(String) String
base64Decode(String) String
tryBase64Decode(String) String
hex(any) String
unhex(String) String
bin(any) String
unbin(String) String
HMAC(String, String, String) String

greatCircleDistance(Float64, Float64, Float64, Float64) Float64
geoDistance(Float64, Float64, Float64, Float64) Float64
pointInEllipses(Float64, Float64, Float64, Float64, Float64, Float64) UInt8
pointInPolygon(any, any) UInt8
geoToH3(Float64, Float64, UInt64) UInt64
h3ToGeo(UInt64) any
h3ToParent(UInt64, UInt64) UInt64
h3GetResolution(UInt64) UInt64
h3IsValid(UInt64) UInt8
h3kRing(UInt64, UInt64) Array(UInt64)
geohashEncode(Float64, Float64, UInt64) String
geohashDecode(String) any
geohashesInBox(Float64, Float64, Float64, Float64, UInt64) Array(String)

toNullable(any) any
ifNotFinite(Float64, Float64) Float64
validateNestedArraySizes(any) UInt8

convertCharset(String, String, String) String
toValidUTF8(String) String
normalizeUTF8NFC(String) String
normalizeUTF8NFD(String) String
normalizeUTF8NFKC(String) String
normalizeUTF8NFKD(String) String

arrayCount(any, Array(any)) UInt32 never-null
arrayExists(any, Array(any)) UInt8 never-null
arrayAll(any, Array(any)) UInt8 never-null
arrayFirstIndex(any, Array(any)) UInt32 never-null
arrayLastIndex(any, Array(any)) UInt32 never-null
arrayFirst(any, Array(any)) any
arrayLast(any, Array(any)) any
arrayFirstOrNull(any, Array(any)) any
arrayLastOrNull(any, Array(any)) any
arrayMin(Array(any)) any
arrayMax(Array(any)) any
arraySum(Array(any)) any
arrayAvg(Array(Float64)) Float64
arrayProduct(Array(Float64)) Float64
arrayZip(Array(any), Array(any)) Array(any)
arrayIntersect(Array(any), Array(any)) Array(any)
arrayWithConstant(UInt64, any) Array(any)
arrayFlatten(Array(any)) any
arrayReverse(Array(any)) any

sequenceMatch(DateTime, any) UInt8 aggregate variadic=2
sequenceMatch(UInt64, any) UInt8 aggregate variadic=2
sequenceCount(DateTime, any) UInt64 aggregate variadic=2
sequenceCount(UInt64, any) UInt64 aggregate variadic=2
windowFunnel(DateTime, any) UInt8 aggregate variadic=2
windowFunnel(UInt64, any) UInt8 aggregate variadic=2
retention(any) Array(UInt8) aggregate variadic=2
sumMap(Array(any), Array(Float64)) any aggregate
minMap(Array(any), Array(Float64)) any aggregate
maxMap(Array(any), Array(Float64)) any aggregate

s3(String, String) any
s3Cluster(String, String, String) any
url(String, String) any
file(String, String) any
hdfs(String, String) any
hudi(String, String) any
iceberg(String, String) any
deltaLake(String, String) any

CRC32(String) UInt64
CRC32IEEE(String) UInt64
CRC64(String) UInt64
adler32(String) UInt64

getMacro(String) String
getSetting(String) any
getServerPort(String) UInt64
getCurrentRoles() Array(String)
hasColumnInTable(String, String, String) UInt8
queryID() String
initialQueryID() String
shardCount() UInt64
shardNum() UInt64
isNullable(any) UInt8

dictGet(String, String, UInt64) any
dictGetString(String, String, UInt64) String
dictGetUInt8(String, String, UInt64) UInt64
dictGetUInt16(String, String, UInt64) UInt64
dictGetUInt32(String, String, UInt64) UInt64
dictGetUInt64(String, String, UInt64) UInt64
dictGetInt8(String, String, UInt64) Int64
dictGetInt16(String, String, UInt64) Int64
dictGetInt32(String, String, UInt64) Int64
dictGetInt64(String, String, UInt64) Int64
dictGetFloat32(String, String, UInt64) Float32
dictGetFloat64(String, String, UInt64) Float64
dictGetUUID(String, String, UInt64) UUID
dictGetDate(String, String, UInt64) Date
dictGetDateTime(String, String, UInt64) DateTime
dictGetOrDefault(String, String, UInt64, any) any
dictGetOrNull(String, String, UInt64) any
dictHas(String, UInt64) UInt8
dictGetHierarchy(String, UInt64) Array(UInt64)
dictGetDescendants(String, UInt64) Array(UInt64)
dictIsIn(String, UInt64, UInt64) UInt8

modelEvaluate(String, any) Float64
evalMLMethod(any, Float64) Float64
sigmoid(Float64) Float64
softmax(Array(Float64)) Array(Float64)
logTrace(Float64) Float64
L1Distance(Array(Float64), Array(Float64)) Float64
L2Distance(Array(Float64), Array(Float64)) Float64
LinfDistance(Array(Float64), Array(Float64)) Float64
cosineDistance(Array(Float64), Array(Float64)) Float64
L1Norm(Array(Float64)) Float64
L2Norm(Array(Float64)) Float64
LinfNorm(Array(Float64)) Float64

reinterpret(any, String) any
reinterpretAsString(any) String
reinterpretAsFixedString(any) FixedString(16)
reinterpretAsDate(any) Date
reinterpretAsDateTime(any) DateTime
reinterpretAsUUID(any) UUID

reinterpretAsInt8(any) Int64
reinterpretAsUInt8(any) UInt64
reinterpretAsInt16(any) Int64
reinterpretAsUInt16(any) UInt64
reinterpretAsInt32(any) Int64
reinterpretAsUInt32(any) UInt64
reinterpretAsInt64(any) Int64
reinterpretAsUInt64(any) UInt64
reinterpretAsInt128(any) Int64
reinterpretAsUInt128(any) UInt64
reinterpretAsInt256(any) Int64
reinterpretAsUInt256(any) UInt64

reinterpretAsFloat32(any) Float32
reinterpretAsFloat64(any) Float64

formatRow(String, any) String variadic=2
formatRowNoNewline(String, any) String variadic=2
toStringOrNull(any) String

toBFloat16(any) BFloat16
toBFloat16OrNull(any) BFloat16
toBFloat16OrZero(any) BFloat16
toBFloat16OrDefault(any) BFloat16

toDateTime32(any) DateTime
toDateTime32OrNull(any) DateTime
toDateTime32OrZero(any) DateTime
toDateTime32OrDefault(any) DateTime

parseDateTimeOrNull(String, String) DateTime
parseDateTimeOrZero(String, String) DateTime

parseDateTime64(String, UInt64) DateTime64
parseDateTime64(String, UInt64, String) DateTime64
parseDateTime64(String, UInt64, String, String) DateTime64
parseDateTime64OrNull(String, UInt64) DateTime64
parseDateTime64OrNull(String, UInt64, String) DateTime64
parseDateTime64OrNull(String, UInt64, String, String) DateTime64
parseDateTime64OrZero(String, UInt64) DateTime64
parseDateTime64OrZero(String, UInt64, String) DateTime64
parseDateTime64OrZero(String, UInt64, String, String) DateTime64

parseDateTime64InJodaSyntax(String, UInt64, String) DateTime64
parseDateTime64InJodaSyntax(String, UInt64, String, String) DateTime64
parseDateTime64InJodaSyntaxOrNull(String, UInt64, String) DateTime64
parseDateTime64InJodaSyntaxOrNull(String, UInt64, String, String) DateTime64
parseDateTime64InJodaSyntaxOrZero(String, UInt64, String) DateTime64
parseDateTime64InJodaSyntaxOrZero(String, UInt64, String, String) DateTime64

fromUnixTimestamp64Second(Int64) DateTime

IPv4ToIPv6(IPv4) FixedString(16)
IPv4CIDRToRange(IPv4, UInt64) Tuple(low IPv4, high IPv4)
IPv6CIDRToRange(IPv6, UInt64) Tuple(low IPv6, high IPv6)
IPv4NumToStringClassC(UInt64) String

IPv4StringToNumOrDefault(String) UInt32
IPv6StringToNumOrDefault(String) String
IPv4StringToNumOrNull(String) UInt32
IPv6StringToNumOrNull(String) String

IPv6NumToString(IPv6) String
cutIPv6(IPv6, UInt64, UInt64) String

isIPAddressInRange(String, String) UInt8
isIPv4String(String) UInt8
isIPv6String(String) UInt8

toIPv4OrNull(String) IPv4
toIPv6OrNull(String) IPv6
toIPv4OrZero(String) IPv4
toIPv6OrZero(String) IPv6
toIPv4OrDefault(String) IPv4
toIPv6OrDefault(String) IPv6

UUIDStringToNum(String) FixedString(16)
UUIDStringToNum(String, UInt64) FixedString(16)
UUIDNumToString(FixedString(16)) String
UUIDNumToString(FixedString(16), UInt64) String
UUIDToNum(UUID) FixedString(16)
UUIDToNum(UUID, UInt64) FixedString(16)

UUIDv7ToDateTime(UUID) DateTime64
UUIDv7ToDateTime(UUID, UInt64) DateTime64
dateTimeToSnowflakeID(DateTime) UInt64
dateTimeToSnowflakeID(DateTime, UInt64) UInt64
dateTime64ToSnowflakeID(DateTime64) UInt64
dateTime64ToSnowflakeID(DateTime64, UInt64) UInt64
dateTimeToUUIDv7(DateTime) UUID

snowflakeToDateTime(UInt64) DateTime
snowflakeToDateTime64(UInt64) DateTime64

toUUIDOrNull(String) UUID
toUUIDOrDefault(String) UUID

multiSearchAllPositions(String, Array(String)) Array(UInt64)
multiSearchFirstIndex(String, Array(String)) UInt64
multiSearchFirstPosition(String, Array(String)) UInt64
multiSearchAllPositionsCaseInsensitive(String, Array(String)) Array(UInt64)
multiSearchFirstIndexCaseInsensitive(String, Array(String)) UInt64
multiSearchFirstPositionCaseInsensitive(String, Array(String)) UInt64
multiSearchAllPositionsUTF8(String, Array(String)) Array(UInt64)
multiSearchFirstIndexUTF8(String, Array(String)) UInt64
multiSearchFirstPositionUTF8(String, Array(String)) UInt64
multiSearchAllPositionsCaseInsensitiveUTF8(String, Array(String)) Array(UInt64)
multiSearchFirstIndexCaseInsensitiveUTF8(String, Array(String)) UInt64
multiSearchFirstPositionCaseInsensitiveUTF8(String, Array(String)) UInt64
multiSearchAnyUTF8(String, Array(String)) UInt8
multiSearchAnyCaseInsensitiveUTF8(String, Array(String)) UInt8

multiMatchAnyIndex(String, Array(String)) UInt64
multiFuzzyMatchAny(String, UInt64, Array(String)) UInt8
multiFuzzyMatchAnyIndex(String, UInt64, Array(String)) UInt64
multiFuzzyMatchAllIndices(String, UInt64, Array(String)) Array(UInt64)
multiMatchAnyIndexCaseInsensitive(String, Array(String)) UInt64
multiFuzzyMatchAnyCaseInsensitive(String, UInt64, Array(String)) UInt8
multiFuzzyMatchAnyIndexCaseInsensitive(String, UInt64, Array(String)) UInt64
multiFuzzyMatchAllIndicesCaseInsensitive(String, UInt64, Array(String)) Array(UInt64)
multiMatchAnyIndexUTF8(String, Array(String)) UInt64
multiFuzzyMatchAnyUTF8(String, UInt64, Array(String)) UInt8
multiFuzzyMatchAnyIndexUTF8(String, UInt64, Array(String)) UInt64
multiFuzzyMatchAllIndicesUTF8(String, UInt64, Array(String)) Array(UInt64)
multiMatchAnyIndexCaseInsensitiveUTF8(String, Array(String)) UInt64
multiFuzzyMatchAnyCaseInsensitiveUTF8(String, UInt64, Array(String)) UInt8
multiFuzzyMatchAnyIndexCaseInsensitiveUTF8(String, UInt64, Array(String)) UInt64
multiFuzzyMatchAllIndicesCaseInsensitiveUTF8(String, UInt64, Array(String)) Array(UInt64)

countMatchesCaseInsensitive(String, String) UInt64
countSubstringsCaseInsensitive(String, String) UInt64
countSubstringsCaseInsensitiveUTF8(String, String) UInt64
extractGroups(String, String) Array(String)
extractAllGroupsHorizontal(String, String) Array(Array(String))

hasAllTokens(String, Array(String)) UInt8
hasAnyTokens(String, Array(String)) UInt8
hasPhrase(String, String) UInt8
hasSubsequence(String, String) UInt8
hasSubsequenceCaseInsensitive(String, String) UInt8
hasSubsequenceUTF8(String, String) UInt8
hasSubsequenceCaseInsensitiveUTF8(String, String) UInt8

ngramDistance(String, String) Float32
ngramSearch(String, String) Float32
ngramDistanceCaseInsensitive(String, String) Float32
ngramSearchCaseInsensitive(String, String) Float32
ngramDistanceUTF8(String, String) Float32
ngramSearchUTF8(String, String) Float32
ngramDistanceCaseInsensitiveUTF8(String, String) Float32
ngramSearchCaseInsensitiveUTF8(String, String) Float32

highlight(String, Array(String)) String
hasTokenCaseInsensitiveOrNull(String, String) UInt64

arrayUnion(Array(any), Array(any)) Array(any)
arrayExcept(Array(any), Array(any)) Array(any)
arraySymmetricDifference(Array(any), Array(any)) Array(any)

arrayShuffle(Array(any)) any
arrayShuffle(Array(any), UInt64) any
arrayRandomSample(Array(any), UInt64) Array(any)
arrayShingles(Array(any), UInt64) Array(Array(any))

arrayPartialSort(Array(any), UInt64) Array(any)
arrayPartialSort(any, Array(any), UInt64) Array(any)
arrayPartialReverseSort(Array(any), UInt64) Array(any)
arrayPartialReverseSort(any, Array(any), UInt64) Array(any)
arrayPartialShuffle(Array(any), UInt64) Array(any)
arrayPartialShuffle(Array(any), UInt64, UInt64) Array(any)

arrayFill(any, Array(any)) any
arrayReverseFill(any, Array(any)) any
arrayRotateLeft(Array(any), Int64) Array(any)
arrayRotateRight(Array(any), Int64) Array(any)
arrayShiftLeft(Array(any), Int64) Array(any)
arrayShiftRight(Array(any), Int64) Array(any)

arrayRemove(Array(any), any) Array(any)
arrayElementOrNull(Array(any), Int64) any

arrayEnumerateDense(Array(any)) Array(UInt64)
arrayEnumerateDenseRanked(Array(any)) Array(UInt64) variadic=1
arrayEnumerateUniq(Array(any)) Array(UInt64) variadic=1
arrayEnumerateUniqRanked(Array(any)) Array(UInt64) variadic=1

arrayDotProduct(Array(Float64), Array(Float64)) Float64
arrayJaccardIndex(Array(any), Array(any)) Float64
arraySimilarity(Array(any), Array(any)) Float64
arrayAutocorrelation(Array(Float64)) Float64
arrayCumSumNonNegative(Array(any)) any

arrayLevenshteinDistance(Array(any), Array(any)) UInt64
arrayLevenshteinDistanceWeighted(Array(any), Array(any), Array(Float64)) Float64

arrayAUCPR(Array(Float64), Array(any)) Float64
arrayROCAUC(Array(Float64), Array(any)) Float64
arrayNormalizedGini(Array(Float64), Array(any)) Float64

arraySplit(any, Array(any)) any
arrayReverseSplit(any, Array(any)) any
arrayReduceInRanges(String, Array(any), Array(any)) Array(any)
arrayTranspose(Array(Array(any))) Array(Array(any))

cutQueryString(String) String
cutFragment(String) String
cutQueryStringAndFragment(String) String
decodeURLComponent(String) String
decodeURLFormComponent(String) String

cutToFirstSignificantSubdomain(String) String
cutToFirstSignificantSubdomainCustom(String) String
cutToFirstSignificantSubdomainCustom(String, String) String
cutToFirstSignificantSubdomainCustomRFC(String) String
cutToFirstSignificantSubdomainCustomRFC(String, String) String
cutToFirstSignificantSubdomainCustomWithWWW(String) String
cutToFirstSignificantSubdomainCustomWithWWW(String, String) String
cutToFirstSignificantSubdomainCustomWithWWWRFC(String) String
cutToFirstSignificantSubdomainCustomWithWWWRFC(String, String) String
cutToFirstSignificantSubdomainRFC(String) String
cutToFirstSignificantSubdomainWithWWW(String) String
cutToFirstSignificantSubdomainWithWWWRFC(String) String

cutURLParameter(String, String) String
cutWWW(String) String

encodeURLComponent(String) String
encodeURLFormComponent(String) String

extractURLParameters(String) Array(String)
extractURLParameterNames(String) Array(String)

firstSignificantSubdomain(String) String
firstSignificantSubdomainCustom(String) String
firstSignificantSubdomainCustomRFC(String) String
firstSignificantSubdomainRFC(String) String

netloc(String) String
port(String) UInt64
port(String, UInt64) UInt64
portRFC(String) UInt64
portRFC(String, UInt64) UInt64
queryStringAndFragment(String) String

URLHierarchy(String) Array(String)
URLPathHierarchy(String) Array(String)
domainRFC(String) String
domainWithoutWWWRFC(String) String
topLevelDomainRFC(String) String

editDistance(String, String) UInt64
editDistanceUTF8(String, String) UInt64
byteEditDistance(String, String) UInt64
levenshteinDistance(String, String) UInt64
damerauLevenshteinDistance(String, String) UInt64
byteHammingDistance(String, String) UInt64
jaroSimilarity(String, String) Float64
jaroWinklerSimilarity(String, String) Float64
stringJaccardIndex(String, String) Float64
stringJaccardIndexUTF8(String, String) Float64

decodeHTMLComponent(String) String
decodeXMLComponent(String) String
encodeXMLComponent(String) String
extractTextFromHTML(String) String

firstLine(String) String
appendTrailingCharIfAbsent(String, String) String
translate(String, String, String) String
translateUTF8(String, String, String) String
overlay(String, String, Int64) String
overlay(String, String, Int64, Int64) String
overlayUTF8(String, String, Int64) String
overlayUTF8(String, String, Int64, Int64) String
substringIndex(String, String, Int64) String
substringIndexUTF8(String, String, Int64) String
naturalSortKey(String) String
soundex(String) String

kolmogorovSmirnovTest(Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
kolmogorovSmirnovTest(Float64, Float64, String) Tuple(statistic Float64, p_value Float64) aggregate
kolmogorovSmirnovTest(Float64, Float64, String, String) Tuple(statistic Float64, p_value Float64) aggregate
mannWhitneyUTest(Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
mannWhitneyUTest(Float64, Float64, String) Tuple(statistic Float64, p_value Float64) aggregate
mannWhitneyUTest(Float64, Float64, String, UInt64) Tuple(statistic Float64, p_value Float64) aggregate
welchTTest(Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
welchTTest(Float64, Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
studentTTest(Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
studentTTest(Float64, Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate

studentTTestOneSample(Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
meanZTest(Float64, Float64, Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
meanZTest(Float64, Float64, Float64, Float64, Float64) Tuple(statistic Float64, p_value Float64) aggregate
proportionsZTest(UInt64, UInt64, UInt64, UInt64) Tuple(statistic Float64, p_value Float64) aggregate
proportionsZTest(UInt64, UInt64, UInt64, UInt64, Float64, String) Tuple(statistic Float64, p_value Float64) aggregate
analysisOfVariance(Float64, UInt64) Tuple(statistic Float64, p_value Float64) aggregate

cramersV(any, any) Float64 aggregate
cramersVBiasCorrected(any, any) Float64 aggregate
theilsU(any, any) Float64 aggregate
contingency(any, any) Float64 aggregate
categoricalInformationValue(any) Float64 never-null aggregate variadic=2
rankCorr(Float64, Float64) Float64 aggregate

argAndMin(any, any) any aggregate
argAndMax(any, any) any aggregate
corrMatrix(Float64) Array(Array(Float64)) never-null aggregate variadic=2
corrStable(Float64, Float64) Float64 aggregate

covarPopMatrix(Float64) Array(Array(Float64)) never-null aggregate variadic=2
covarPopStable(Float64, Float64) Float64 aggregate
covarSampMatrix(Float64) Array(Array(Float64)) never-null aggregate variadic=2
covarSampStable(Float64, Float64) Float64 aggregate

varPopStable(Float64) Float64 aggregate
varSampStable(Float64) Float64 aggregate
stddevPopStable(Float64) Float64 aggregate
stddevSampStable(Float64) Float64 aggregate

stochasticLogisticRegression(Float64, Float64) Array(Float64) aggregate
distinctDynamicTypes(any) Array(String) aggregate
distinctJSONPaths(JSON) Array(String) aggregate
distinctJSONPathsAndTypes(JSON) Array(String) aggregate
estimateCompressionRatio(any) Float64 aggregate
flameGraph(Array(UInt64)) Array(String) aggregate
flameGraph(Array(UInt64), UInt64) Array(String) aggregate
flameGraph(Array(UInt64), UInt64, UInt64) Array(String) aggregate
groupArrayArray(Array(any)) Array(any) aggregate
groupArrayIntersect(Array(any)) Array(any) aggregate
maxIntersections(any, any) any aggregate
maxIntersectionsPosition(any, any) any aggregate

sparkbar(any, any) String aggregate
quantileDD(Float64) Float64 aggregate
quantileExactExclusive(Float64) Float64 aggregate
quantileExactInclusive(Float64) Float64 aggregate
quantileExactHigh(any) any aggregate
quantileExactLow(any) any aggregate
quantileExactWeightedInterpolated(Float64, UInt64) Float64 aggregate
quantilePrometheusHistogram(Float64, Float64) Float64 aggregate
quantilesExactExclusive(Float64) Array(Float64) aggregate
quantilesExactInclusive(Float64) Array(Float64) aggregate
quantilesExact(any) any aggregate
quantilesExactHigh(any) any aggregate
quantilesExactLow(any) any aggregate
quantilesGK(Float64) Array(Float64) aggregate
quantilesTimingWeighted(Float64, UInt64) Array(Float32) aggregate
quantileTDigestWeighted(Float64, UInt64) Float32 aggregate
quantilesTDigest(Float64) Array(Float32) aggregate
quantilesTiming(Float64) Array(Float32) aggregate

sumMapWithOverflow(Array(any), Array(Float64)) any aggregate
sumMapFiltered(Array(any), Array(Float64)) any aggregate
sumMapFilteredWithOverflow(Array(any), Array(Float64)) any aggregate

timeSeriesChangesToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesChangesToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesDeltaToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesDeltaToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesDerivToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesDerivToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesGroupArray(DateTime, Float64) Array(Float64) aggregate
timeSeriesGroupArray(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesInstantDeltaToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesInstantDeltaToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesInstantRateToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesInstantRateToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesLastTwoSamples(DateTime, Float64) Array(Float64) aggregate
timeSeriesLastTwoSamples(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesPredictLinearToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesPredictLinearToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesRateToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesRateToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesResampleToGridWithStaleness(DateTime, Float64) Array(Float64) aggregate
timeSeriesResampleToGridWithStaleness(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate
timeSeriesResetsToGrid(DateTime, Float64) Array(Float64) aggregate
timeSeriesResetsToGrid(UInt64, UInt64, UInt64, UInt64, DateTime, Float64) Array(Float64) aggregate

sequenceMatchEvents(String, DateTime, Bool) Array(String) never-null aggregate variadic=3
sequenceNextNode(DateTime, any, Bool) any never-null aggregate variadic=3
uniqUpTo(any) UInt64 aggregate

bitmapBuild(Array(UInt64)) any
bitmapToArray(any) Array(UInt64)
bitmapSubsetInRange(any, UInt64, UInt64) any
bitmapSubsetLimit(any, UInt64, UInt64) any
subBitmap(any, UInt64, UInt64) any
bitmapTransform(any, Array(UInt64), Array(UInt64)) any

bitmapContains(any, UInt64) UInt8
bitmapHasAll(any, any) UInt8
bitmapHasAny(any, any) UInt8
bitmapMin(any) UInt64
bitmapMax(any) UInt64
bitmapCardinality(any) UInt64

bitmapAnd(any, any) any
bitmapOr(any, any) any
bitmapXor(any, any) any
bitmapAndnot(any, any) any
bitmapAndCardinality(any, any) UInt64
bitmapOrCardinality(any, any) UInt64
bitmapXorCardinality(any, any) UInt64
bitmapAndnotCardinality(any, any) UInt64

groupBitmap(UInt64) UInt64 aggregate
groupBitmapAnd(any) UInt64 aggregate
groupBitmapOr(any) UInt64 aggregate
groupBitmapXor(any) UInt64 aggregate

LpDistance(Array(Float64), Array(Float64), Float64) Float64
LpNorm(Array(Float64), Float64) Float64
L2SquaredDistance(Array(Float64), Array(Float64)) Float64
L2SquaredNorm(Array(Float64)) Float64
L2DistanceTransposed(Array(Float64), Array(Float64), Float64) Float64
cosineDistanceTransposed(Array(Float64), Array(Float64), Float64) Float64

L1Normalize(Array(Float64)) Array(Float64)
L2Normalize(Array(Float64)) Array(Float64)
LinfNormalize(Array(Float64)) Array(Float64)
LpNormalize(Array(Float64), Float64) Array(Float64)

tupleHammingDistance(any, any) UInt64
arrayHammingDistance(Array(any), Array(any)) UInt64
bitHammingDistance(Int64, Int64) UInt64

detectCharset(String) String
detectLanguage(String) String
detectLanguageMixed(String) Map(String, Float32)
detectLanguageUnknown(String) String
stem(String, String) String
lemmatize(String, String) String
synonyms(String, String) Array(String)
detectTonality(String) Float32

geoToS2(Float64, Float64) UInt64
s2ToGeo(UInt64) Tuple(lon Float64, lat Float64)
s2GetNeighbors(UInt64) Array(UInt64)
s2CellsIntersect(UInt64, UInt64) UInt8
s2CapContains(UInt64, Float64, UInt64) UInt8
s2CapUnion(UInt64, Float64, UInt64, Float64) Tuple(centre UInt64, radius Float64)
s2RectAdd(UInt64, UInt64, UInt64) Tuple(lo UInt64, hi UInt64)
s2RectContains(UInt64, UInt64, UInt64) UInt8
s2RectUnion(UInt64, UInt64, UInt64, UInt64) Tuple(lo UInt64, hi UInt64)
s2RectIntersection(UInt64, UInt64, UInt64, UInt64) Tuple(lo UInt64, hi UInt64)

readWKTPoint(String) Point
readWKTLineString(String) LineString
readWKTPolygon(String) Polygon
readWKTMultiPolygon(String) MultiPolygon
readWKTRing(String) Ring

readWKBPoint(String) Point
readWKBLineString(String) LineString
readWKBPolygon(String) Polygon
readWKBMultiPolygon(String) MultiPolygon
readWKBMultiLineString(String) MultiLineString

wkt(any) String

polygonsIntersectionCartesian(any, any) MultiPolygon
polygonsIntersectionSpherical(any, any) MultiPolygon
polygonsUnionCartesian(any, any) MultiPolygon
polygonsUnionSpherical(any, any) MultiPolygon
polygonsSymDifferenceCartesian(any, any) MultiPolygon
polygonsSymDifferenceSpherical(any, any) MultiPolygon
polygonsIntersectCartesian(any, any) UInt8
polygonsIntersectSpherical(any, any) UInt8
polygonsWithinCartesian(any, any) UInt8
polygonsWithinSpherical(any, any) UInt8
polygonsEqualsCartesian(any, any) UInt8
polygonConvexHullCartesian(any) Polygon

polygonAreaCartesian(any) Float64
polygonAreaSpherical(any) Float64
polygonPerimeterCartesian(any) Float64
polygonPerimeterSpherical(any) Float64
polygonsDistanceCartesian(any, any) Float64
polygonsDistanceSpherical(any, any) Float64

h3EdgeAngle(UInt64) Float64
h3EdgeLengthM(UInt64) Float64
h3EdgeLengthKm(UInt64) Float64
h3ExactEdgeLengthM(UInt64) Float64
h3ExactEdgeLengthKm(UInt64) Float64
h3ExactEdgeLengthRads(UInt64) Float64

h3HexAreaM2(UInt64) Float64
h3HexAreaKm2(UInt64) Float64
h3CellAreaM2(UInt64) Float64
h3CellAreaRads2(UInt64) Float64
h3NumHexagons(UInt64) UInt64
h3GetBaseCell(UInt64) UInt64
h3GetFaces(UInt64) Array(UInt64)

h3ToChildren(UInt64, UInt64) Array(UInt64)
h3ToCenterChild(UInt64, UInt64) UInt64
h3ToString(UInt64) String
stringToH3(String) UInt64
h3GetRes0Indexes() Array(UInt64)
h3GetPentagonIndexes(UInt64) Array(UInt64)
h3ToGeoBoundary(UInt64) Array(Tuple(lon Float64, lat Float64))

h3IsResClassIII(UInt64) UInt8
h3IsPentagon(UInt64) UInt8
h3IndexesAreNeighbors(UInt64, UInt64) UInt8

h3Line(UInt64, UInt64) Array(UInt64)
h3Distance(UInt64, UInt64) UInt64
h3HexRing(UInt64, UInt64) Array(UInt64)
h3PointDistM(Float64, Float64, Float64, Float64) Float64
h3PointDistKm(Float64, Float64, Float64, Float64) Float64
h3PointDistRads(Float64, Float64, Float64, Float64) Float64
h3PolygonToCells(any, UInt64) Array(UInt64)
h3PolygonToCellsWithContainment(any, UInt64, UInt64) Array(UInt64)

h3GetUnidirectionalEdge(UInt64, UInt64) UInt64
h3UnidirectionalEdgeIsValid(UInt64) UInt8
h3GetOriginIndexFromUnidirectionalEdge(UInt64) UInt64
h3GetDestinationIndexFromUnidirectionalEdge(UInt64) UInt64
h3GetIndexesFromUnidirectionalEdge(UInt64) Array(UInt64)
h3GetUnidirectionalEdgesFromHexagon(UInt64) Array(UInt64)
h3GetUnidirectionalEdgeBoundary(UInt64) Array(Tuple(lon Float64, lat Float64))

sinh(Float64) Float64
cosh(Float64) Float64
tanh(Float64) Float64
asinh(Float64) Float64
acosh(Float64) Float64
atanh(Float64) Float64
erf(Float64) Float64
erfc(Float64) Float64
lgamma(Float64) Float64
tgamma(Float64) Float64

exp2(Float64) Float64
exp10(Float64) Float64
log1p(Float64) Float64
degrees(Float64) Float64
radians(Float64) Float64
intExp2(Int64) UInt64
intExp10(Int64) UInt64
hypot(Float64, Float64) Float64

isPrime(UInt64) UInt8
isProbablePrime(UInt64) UInt8

ceiling(Float64) any
ceiling(Float64, Int64) any
trunc(Float64) any
trunc(Float64, Int64) any
roundAge(UInt64) UInt64
roundDuration(UInt64) UInt64
roundDown(Float64, Array(Float64)) Float64
roundToExp2(UInt64) UInt64

bitSlice(String, UInt64) String
bitSlice(String, UInt64, UInt64) String

MD4(String) FixedString(16)
SHA512_256(String) FixedString(32)
RIPEMD160(String) FixedString(20)
keccak256(String) FixedString(32)
BLAKE3(String) FixedString(32)

xxh3(any) UInt64
xxh3_128(any) FixedString(16)
wyHash64(any) UInt64
metroHash64(any) UInt64
intHash32(any) UInt32
murmurHash2_32(any) UInt32
murmurHash3_128(any) FixedString(16)
farmFingerprint64(any) UInt64
URLHash(String) UInt64
URLHash(String, UInt64) UInt64

sipHash64Keyed(any, any) UInt64
sipHash128(any) FixedString(16)
sipHash128Keyed(any, any) FixedString(16)
sipHash128Reference(any) FixedString(16)
sipHash128ReferenceKeyed(any, any) FixedString(16)

hiveHash(String) UInt64
icebergHash(String) Int32
javaHash(String) Int32
javaHashUTF16LE(String) Int32
jumpConsistentHash(UInt64, Int32) Int32
kostikConsistentHash(UInt64, Int32) Int32
kafkaMurmurHash(String) Int32
gccMurmurHash(String) Int64

ngramMinHash(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
ngramMinHashCaseInsensitive(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
ngramMinHashUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
ngramMinHashCaseInsensitiveUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
ngramSimHash(String, UInt64) UInt64
ngramSimHashCaseInsensitive(String, UInt64) UInt64
ngramSimHashUTF8(String, UInt64) UInt64
ngramSimHashCaseInsensitiveUTF8(String, UInt64) UInt64
wordShingleMinHash(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashCaseInsensitive(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashCaseInsensitiveUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashArg(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashArgCaseInsensitive(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashArgUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleMinHashArgCaseInsensitiveUTF8(String, UInt64, UInt64) Tuple(min UInt64, max UInt64)
wordShingleSimHash(String, UInt64) UInt64
wordShingleSimHashCaseInsensitive(String, UInt64) UInt64
wordShingleSimHashUTF8(String, UInt64) UInt64
wordShingleSimHashCaseInsensitiveUTF8(String, UInt64) UInt64

JSONArrayLength(String) UInt64
JSONExtractKeysAndValues(String, String) Array(Tuple(key String, value String))
JSONExtractKeysAndValues(String, String, String) Array(Tuple(key String, value String))
JSONExtractKeysAndValuesRaw(String) Array(Tuple(key String, value String))
JSONExtractKeysAndValuesRaw(String, String) Array(Tuple(key String, value String))

JSONAllPaths(JSON) Array(String)
JSONAllPathsWithTypes(JSON) Array(String)
JSONAllValues(JSON) Array(String)
JSONDynamicPaths(JSON) Array(String)
JSONDynamicPathsWithTypes(JSON) Array(String)
JSONSharedDataPaths(JSON) Array(String)
JSONSharedDataPathsWithTypes(JSON) Array(String)

JSONExtractCaseInsensitive(String, String, String) any
JSONExtractStringCaseInsensitive(String, String) String
JSONExtractIntCaseInsensitive(String, String) Int64
JSONExtractUIntCaseInsensitive(String, String) UInt64
JSONExtractFloatCaseInsensitive(String, String) Float64
JSONExtractBoolCaseInsensitive(String, String) UInt8
JSONExtractRawCaseInsensitive(String, String) String
JSONExtractArrayRawCaseInsensitive(String, String) Array(String)
JSONExtractKeysCaseInsensitive(String, String) Array(String)
JSONExtractKeysAndValuesCaseInsensitive(String, String) Array(Tuple(key String, value String))
JSONExtractKeysAndValuesRawCaseInsensitive(String) Array(Tuple(key String, value String))
JSONExtractKeysAndValuesRawCaseInsensitive(String, String) Array(Tuple(key String, value String))

prettyPrintJSON(String) String
toJSONString(any) String
JSONMergePatch(String) JSON variadic=1

dynamicElement(any, String) any
dynamicType(any) String
isDynamicElementInSharedData(any) UInt8

tupleConcat(any, any) any
tupleNames(any) Array(String)
flattenTuple(any) any
tupleToNameValuePairs(any) Array(Tuple(key String, value String))

tupleNegate(any) any
tuplePlus(any, any) any
tupleMinus(any, any) any
tupleMultiply(any, any) any
tupleDivide(any, any) any
tupleMultiplyByNumber(any, Float64) any
tupleDivideByNumber(any, Float64) any
tupleIntDiv(any, any) any
tupleIntDivOrZero(any, any) any
tupleModulo(any, any) any
tupleIntDivByNumber(any, Float64) any
tupleIntDivOrZeroByNumber(any, Float64) any
tupleModuloByNumber(any, Float64) any
tuplePositiveModuloByNumber(any, Float64) any
dotProduct(any, any) Float64

mapConcat(any) any variadic=1
mapFromArrays(Array(any), Array(any)) any
mapUpdate(any, any) any
mapPopulateSeries(any) any
mapPopulateSeries(any, any) any

mapAll(any) UInt8 variadic=1
mapExists(any) UInt8 variadic=1
mapSort(any) any
mapSort(any, any) any
mapReverseSort(any) any
mapReverseSort(any, any) any
mapPartialSort(UInt64, any) any
mapPartialSort(any, UInt64, any) any
mapPartialReverseSort(UInt64, any) any
mapPartialReverseSort(any, UInt64, any) any

mapContainsKey(any, any) UInt8
mapContainsKeyLike(any, String) UInt8
mapContainsValue(any, any) UInt8
mapContainsValueLike(any, String) UInt8

mapExtractKeyLike(any, String) any
mapExtractValueLike(any, String) any

extractKeyValuePairs(String) any
extractKeyValuePairs(String, String) any
extractKeyValuePairs(String, String, String) any
extractKeyValuePairs(String, String, String, String) any
extractKeyValuePairsWithEscaping(String) any
extractKeyValuePairsWithEscaping(String, String) any
extractKeyValuePairsWithEscaping(String, String, String) any
extractKeyValuePairsWithEscaping(String, String, String, String) any

char(UInt64) String variadic=1
bitmaskToArray(Int64) Array(UInt64)
bitmaskToList(Int64) String
bitPositionsToArray(Int64) Array(UInt64)

mortonEncode(UInt64) UInt64 variadic=1
mortonDecode(UInt64, UInt64) any
hilbertEncode(UInt64) UInt64 variadic=1
hilbertDecode(UInt64, UInt64) any

sqidEncode(UInt64) String variadic=1
sqidDecode(String) Array(UInt64)
bech32Encode(String, String) String
bech32Decode(String) any

base32Encode(String) String
base32Decode(String) String
tryBase32Decode(String) String
base58Encode(String) String
base58Decode(String) String
tryBase58Decode(String) String
base64URLEncode(String) String
base64URLDecode(String) String
tryBase64URLDecode(String) String

idnaDecode(String) String
idnaEncode(String) String
tryIdnaEncode(String) String
punycodeDecode(String) String
punycodeEncode(String) String
tryPunycodeDecode(String) String

tryDecrypt(String, String, String) String
tryDecrypt(String, String, String, String) String
tryDecrypt(String, String, String, String, String) String

randomString(UInt64) String
randomStringUTF8(UInt64) String
randomPrintableASCII(UInt64) String
randomFixedString(UInt64) String
fuzzBits(String, Float64) String
randNegativeBinomial(UInt64, Float64) UInt64

in(any, any) UInt8
notIn(any, any) UInt8
globalIn(any, any) UInt8
globalNotIn(any, any) UInt8

nonNegativeDerivative(Float64, DateTime) Float64
nonNegativeDerivative(Float64, DateTime, String) Float64
