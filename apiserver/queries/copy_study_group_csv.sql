copy (
    select
        sg.semester_season as saison,
        sg.semester_year as jahr,
        sg.student_count as studenten_anzahl,
        dp.name as studiengang,
        sg.extra_data as extra_daten
    from
        study_group sg
        join degree_program dp on sg.degree_program = dp.id)
    to stdout csv header
