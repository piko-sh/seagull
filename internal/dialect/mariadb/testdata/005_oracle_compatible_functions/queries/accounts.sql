-- sgl.query(name: DescribeAccounts, command: many)
SELECT
    id,
    NVL(nickname, name) AS display_name,
    NVL(overdraft, 0) AS overdraft_limit,
    NVL(nickname, nickname) AS maybe_nickname,
    NVL2(overdraft, balance, 0) AS credit,
    DECODE_ORACLE(id, 1, 'first', 2, 'second') AS ordinal,
    DECODE_ORACLE(id, 1, 'first', 'other') AS ordinal_or_other,
    ADD_MONTHS(opened_at, 12) AS renewal_at,
    TO_CHAR(opened_at, 'YYYY-MM-DD') AS opened_on,
    LENGTHB(name) AS name_bytes,
    SFORMAT('{} ({})', name, id) AS label,
    NATURAL_SORT_KEY(name) AS sort_key,
    CRC32C(name) AS checksum,
    UUID_V7() AS request_id,
    JSON_OBJECT_TO_ARRAY(profile) AS profile_pairs,
    LTRIM_ORACLE(name) AS trimmed_name
FROM accounts;
