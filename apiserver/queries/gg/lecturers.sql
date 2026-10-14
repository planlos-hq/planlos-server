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
order by
    last_name,
    first_name
