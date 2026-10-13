# Dot-source this file from labs: . ./cmd/tests/recipe-demo.ps1
# Defining these functions does not write any files.

function Get-RecipeText {
    param([ValidateRange(1, 10)][int]$Version)

    $changes = @(
        'Initial blueberry pie recipe.',
        'Reduce sugar from 150 g to 120 g.',
        'Add 1 tablespoon lemon juice to the filling.',
        'Add 1 teaspoon lemon zest for extra flavor.',
        'Increase cornstarch from 2 to 3 tablespoons for a thicker filling.',
        'Chill the dough for 30 minutes before rolling.',
        'Bake at 190 C instead of 180 C.',
        'Increase baking time from 40 to 45 minutes.',
        'Brush the top crust with beaten egg before baking.',
        'Cool for 2 hours before slicing so the filling sets.'
    )
    $sugar = if ($Version -ge 2) { 120 } else { 150 }
    $starch = if ($Version -ge 5) { 3 } else { 2 }
    $temperature = if ($Version -ge 7) { 190 } else { 180 }
    $minutes = if ($Version -ge 8) { 45 } else { 40 }
    $lines = @(
        'BLUEBERRY PIE',
        "Version: $Version",
        "Change: $($changes[$Version - 1])",
        '',
        'Ingredients:',
        '- 500 g blueberries',
        "- $sugar g sugar",
        "- $starch tablespoons cornstarch",
        '- 300 g flour',
        '- 150 g cold butter',
        '- 5 tablespoons cold water'
    )
    if ($Version -ge 3) { $lines += '- 1 tablespoon lemon juice' }
    if ($Version -ge 4) { $lines += '- 1 teaspoon lemon zest' }
    if ($Version -ge 9) { $lines += '- 1 beaten egg for brushing' }
    $lines += @('', 'Method:', '1. Rub the butter into the flour, then mix in the cold water to form dough.')
    if ($Version -ge 6) { $lines += '2. Chill the dough for 30 minutes, then roll it out.' }
    else { $lines += '2. Roll out the dough.' }
    $lines += '3. Line a pie dish with half the dough. Reserve the rest for the top.'
    $filling = '4. Mix blueberries, sugar and cornstarch'
    if ($Version -ge 3) { $filling += ', lemon juice' }
    if ($Version -ge 4) { $filling += ' and lemon zest' }
    $lines += "$filling. Spoon the filling into the pie dish."
    $lines += '5. Cover with the remaining dough and cut small steam vents.'
    if ($Version -ge 9) { $lines += '6. Brush the top crust with beaten egg.' }
    else { $lines += '6. Leave the top crust unglazed.' }
    $lines += "7. Bake in a preheated oven at $temperature C for $minutes minutes, until golden and bubbling."
    if ($Version -ge 10) { $lines += '8. Cool for 2 hours before slicing and serving.' }
    else { $lines += '8. Let the pie cool before serving.' }
    return $lines -join "`n"
}

function Save-Recipe {
    param([ValidateRange(1, 10)][int]$Version)
    $recipePath = Join-Path $PWD 'cli-check/release.txt'
    [IO.File]::WriteAllText($recipePath, (Get-RecipeText $Version))
    Write-Host "Updated file: $recipePath"
    Write-Host ([IO.File]::ReadAllText($recipePath))
    Write-Host "SHA-256: $((Get-FileHash -LiteralPath $recipePath -Algorithm SHA256).Hash.ToLowerInvariant())"
}

function Update-Release {
    param([ValidateRange(1, 10)][int]$Version)
    Save-Recipe $Version
}
