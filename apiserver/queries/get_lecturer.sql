select
    id,
    first_name,
    last_name,
    abbreviation,
    kind,
    notes,
    isRelevant,
    extra_data,
    title
from
    lecturer
where
    id = $1
