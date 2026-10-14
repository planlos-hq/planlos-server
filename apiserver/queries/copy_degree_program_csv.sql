copy (
    select
        name as name,
        abbreviation as kürzel,
        extra_data as extra_daten
    from
        degree_program)
    to stdout csv header
