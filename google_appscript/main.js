// TODO: Replace this will the URL of the app server when deployed.
const url = "https://api.chainparse.orijtech.com/";

function fetchAndPopulateSpreadsheet() {
    var response = UrlFetchApp.fetch(url);
    var data = JSON.parse(response.getContentText());

    sheetNames = ["Projects", "LatestProjects"];

    sheetNames.forEach(function(sheetName) {
        var sheet = SpreadsheetApp.getActive().getSheetByName(sheetName);
        var dataRanges = sheet.getDataRange().getValues();

        dataRanges.forEach(function(row, zero_col) {
            var column = zero_col+1;
            if (column < 5) return;

            var ref = data;
            if (sheetName === "LatestProjects")
                ref = data.Latest;
      
            var chainName = row[0];
            var got = ref[chainName] || ref[chainName.toLowerCase()] || ref[chainName.toUpperCase()] || null;
            if (got !== null) {
                sheet.getRange(column, 8, 1, 1).setValue(got.cosmos_sdk_version||"");
                sheet.getRange(column, 10, 1, 1).setValue(got.tendermint_version||"");
                sheet.getRange(column, 11, 1, 1).setValue(got.ibc_version||"");
                sheet.getRange(column, 6, 1, 1).setValue(got.is_mainnet||"");
            } else {
                console.log("could not retrieve chain data for: "+chainName);
                got = {};
            }

            var codebase = got.codebase || null;
            if (!codebase) return;

            sheet.getRange(column, 2, 1, 1).setValue(codebase.git_repo);
            sheet.getRange(column, 7, 1, 1).setValue(codebase.recommended_version);
        });
    });
}
