copy (
    select
        number as nummer,
        name as name,
        seats as plätze,
        extra_data as extra_daten
    from
        room)
    to stdout csv header
