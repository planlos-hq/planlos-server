insert into study_group (semester_season, semester_year, student_count,
    degree_program, extra_data)
    values ($1, $2, $3, (
            select
                id
            from
                degree_program
            where
                lower(name) = lower($4)
                or lower(abbreviation) = lower($4)
            order by
                (name = $4) desc
            limit 1),
        $5)
returning
    id;
