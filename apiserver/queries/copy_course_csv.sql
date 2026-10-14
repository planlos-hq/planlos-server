copy (
    select
        number as nummer,
        name as name,
        sws as sws,
        credit_points as credit_points,
        abbreviation as kürzel,
        extra_data as extra_daten
    from
        course)
    to stdout csv header
