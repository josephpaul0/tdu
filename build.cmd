@echo off
go version
echo Compiling tdu.exe...
go build -a -ldflags "-s -w"
if %ERRORLEVEL% == 0 (
    echo SUCCESSFUL
) ELSE (
    echo FAILED
)
echo Press any key to exit...
pause > nul
