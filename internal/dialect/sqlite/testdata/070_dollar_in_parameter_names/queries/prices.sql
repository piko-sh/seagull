-- sgl.query(PricesBetween, many)
SELECT id, label, amount$usd FROM prices WHERE amount$usd >= :low$usd AND amount$usd < $high$usd AND label <> @skip$;
