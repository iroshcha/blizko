param([Parameter(Mandatory=$true)][string]$Path)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$images = @()
foreach ($size in @(16,32,48,64,128,256)) {
    $bitmap = New-Object System.Drawing.Bitmap($size,$size)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $green = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::FromArgb(35,108,88))
    $white = New-Object System.Drawing.SolidBrush([System.Drawing.Color]::White)
    $graphics.FillEllipse($green,0,0,$size,$size)
    $graphics.FillEllipse($white,($size*.17),($size*.24),($size*.66),($size*.48))
    $points = [System.Drawing.PointF[]]@([System.Drawing.PointF]::new($size*.28,$size*.58),[System.Drawing.PointF]::new($size*.25,$size*.80),[System.Drawing.PointF]::new($size*.48,$size*.64))
    $graphics.FillPolygon($white,$points)
    foreach ($x in @(.35,.50,.65)) { $graphics.FillEllipse($green,($size*($x-.04)),($size*.445),($size*.08),($size*.08)) }
    $stream = New-Object System.IO.MemoryStream
    $bitmap.Save($stream,[System.Drawing.Imaging.ImageFormat]::Png)
    $images += ,$stream.ToArray()
    $stream.Dispose(); $graphics.Dispose(); $bitmap.Dispose(); $green.Dispose(); $white.Dispose()
}
$file = [System.IO.File]::Create($Path)
$writer = New-Object System.IO.BinaryWriter($file)
try {
    $writer.Write([uint16]0);$writer.Write([uint16]1);$writer.Write([uint16]$images.Count)
    $offset = 6 + 16*$images.Count
    $sizes = @(16,32,48,64,128,256)
    for ($i=0;$i -lt $images.Count;$i++) {
        $dimension = if($sizes[$i] -eq 256){0}else{$sizes[$i]}
        $writer.Write([byte]$dimension);$writer.Write([byte]$dimension);$writer.Write([byte]0);$writer.Write([byte]0)
        $writer.Write([uint16]1);$writer.Write([uint16]32);$writer.Write([uint32]$images[$i].Length);$writer.Write([uint32]$offset)
        $offset += $images[$i].Length
    }
    foreach($image in $images){$writer.Write([byte[]]$image)}
} finally {$writer.Dispose();$file.Dispose()}
