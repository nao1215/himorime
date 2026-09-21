# Sums the amount column of generated CSV per city and prints, for each city,
# its name, its number of rows and its total with two decimals. The note
# column can hold a quoted comma, but it is the last one, so splitting on
# every comma still leaves city and amount in fields 3 and 4.
BEGIN { FS = "," }
NR > 1 { total[$3] += $4; rows[$3]++ }
END { for (city in total) printf "%s %d %.2f\n", city, rows[city], total[city] }
