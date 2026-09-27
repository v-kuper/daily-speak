package media

import "daily-speaking-practice/backend/internal/storage"

func signedRequestFromStorage(request storage.PresignedRequest) SignedRequest {
	headers := make(map[string][]string, len(request.Headers))
	for name, values := range request.Headers {
		headers[name] = append([]string(nil), values...)
	}
	return SignedRequest{
		Method: request.Method, URL: request.URL, Headers: headers, ExpiresAt: request.ExpiresAt,
	}
}

func uploadedPartsFromStorage(parts []storage.PartInfo) []UploadedPart {
	result := make([]UploadedPart, 0, len(parts))
	for _, part := range parts {
		result = append(result, uploadedPartFromStorage(part))
	}
	return result
}

func uploadedPartFromStorage(part storage.PartInfo) UploadedPart {
	return UploadedPart{
		PartNumber: int(part.Number), SizeBytes: part.Size, ETag: part.ETag,
		ChecksumSHA256: part.SHA256, LastModified: part.LastModified,
	}
}

func contentInfoFromStorage(info storage.ObjectInfo) ContentInfo {
	return ContentInfo{
		ContentType: info.ContentType, SizeBytes: info.Size, ETag: info.ETag, LastModified: info.LastModified,
	}
}

func verifiedObjectFromStorage(info storage.ObjectInfo) VerifiedObject {
	return VerifiedObject{SizeBytes: info.Size, ChecksumSHA256: info.SHA256, ETag: info.ETag}
}
