copy (
    select
        title as titel,
        first_name as vorname,
        last_name as nachname,
        abbreviation as kürzel,
        kind as art,
        notes as notizen,
        extra_data as extra_daten
    from
        lecturer)
    to stdout csv header
